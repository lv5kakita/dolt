// Copyright 2020 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package querydiff

import (
	"io"
	"math"

	"github.com/liquidata-inc/dolt/go/store/atomicerr"

	"github.com/liquidata-inc/go-mysql-server/sql"
	"github.com/liquidata-inc/go-mysql-server/sql/plan"
)

type rowCmp int32

const (
	lesser  rowCmp = -1
	equal   rowCmp = 0
	greater rowCmp = 1
	unknown rowCmp = math.MaxInt32
)

// nodeDiffer is used to create a modified query plan to be used by
// QueryDiffer to diff a query.
// Given two identical query plans ("from", "to") created from two
// RootValues, a nodeDiffer is injected at the query plan Node where
// the query results are "orderable". The nodeDiffer iterates over
// the child query nodes, diffs their results and passes the diffed
// rows to its parent nodes through synthetic "from" and "to" Nodes.
type nodeDiffer interface {
	makeFromNode() sql.Node
	makeToNode() sql.Node
}

func newSortNodeDiffer(fromCtx, toCtx *sql.Context, from, to *plan.Sort) (nodeDiffer, error) {
	fromIter, err := from.RowIter(fromCtx)
	if err != nil {
		return nil, err
	}

	toIter, err := to.RowIter(toCtx)
	if err != nil {
		return nil, err
	}

	ae := atomicerr.New()

	return &sortNodeDiffer{
		fromChild: from,
		toChild:   to,
		fromIter:  newIterQueue(fromIter, ae),
		toIter:    newIterQueue(toIter, ae),
		lastCmp:   unknown,
		ae:        ae,
	}, nil
}

type sortNodeDiffer struct {
	ctx       *sql.Context
	fromChild *plan.Sort
	toChild   *plan.Sort
	fromIter  *iterQueue
	toIter    *iterQueue
	lastCmp   rowCmp
	ae        *atomicerr.AtomicError
}

var _ nodeDiffer = &sortNodeDiffer{}

func (nd *sortNodeDiffer) nextFromRow() (sql.Row, error) {
	nd.fromIter.maybeStart()
	nd.toIter.maybeStart()

	if nd.fromIter.isDone() {
		return nil, io.EOF
	}
	if nd.toIter.isDone() {
		return nd.fromIter.pop(), nil
	}

	if nd.lastCmp != unknown {
		panic("query diff iterators called out of order")
	}

	var err error
	nd.lastCmp, err = nd.rowCompare(nd.fromIter.peek(), nd.toIter.peek())
	if err != nil {
		return nil, err
	}

	switch nd.lastCmp {
	case lesser:
		return nd.fromIter.pop(), nil
	case equal:
		return nd.fromIter.pop(), nil
	case greater:
		return nil, errSkip
	default:
		panic("incorrect value fromIter rowCmp")
	}
}

func (nd *sortNodeDiffer) nextToRow() (sql.Row, error) {
	nd.fromIter.maybeStart()
	nd.toIter.maybeStart()

	if nd.toIter.isDone() {
		return nil, io.EOF
	}
	if nd.fromIter.isDone() && nd.lastCmp == unknown {
		return nd.toIter.pop(), nil
	}
	// if lastCmp != unknown, fromIter just popped its last item

	if nd.lastCmp == unknown {
		panic("query diff iterators called out of order")
	}

	cmp := unknown
	nd.lastCmp, cmp = cmp, nd.lastCmp

	switch cmp {
	case lesser:
		return nil, errSkip
	case equal:
		return nd.toIter.pop(), nil
	case greater:
		return nd.toIter.pop(), nil
	default:
		panic("incorrect value fromIter rowCmp")
	}
}

func (nd *sortNodeDiffer) rowCompare(left, right sql.Row) (rowCmp, error) {
	if left == nil || right == nil {
		panic("nil rows cannot be compared")
	}

	for _, sf := range nd.fromChild.SortFields {
		typ := sf.Column.Type()
		lv, err := sf.Column.Eval(nd.ctx, left)
		if err != nil {
			return unknown, err
		}

		rv, err := sf.Column.Eval(nd.ctx, right)
		if err != nil {
			return unknown, err
		}

		if sf.Order == plan.Descending {
			lv, rv = rv, lv
		}

		if lv == nil && rv == nil {
			continue
		} else if lv == nil {
			if sf.NullOrdering == plan.NullsFirst {
				return lesser, nil
			} else {
				return greater, nil
			}
		} else if rv == nil {
			if sf.NullOrdering == plan.NullsFirst {
				return greater, nil
			} else {
				return lesser, nil
			}
		}

		cmp, err := typ.Compare(lv, rv)
		if err != nil {
			return 0, err
		}
		if cmp != 0 {
			return rowCmp(cmp), nil
		}
	}
	return 0, nil
}

type sqlNodeWrapper struct {
	sql.Node
	iter rowIterWrapper
}

var _ sql.Node = sqlNodeWrapper{}

func (w sqlNodeWrapper) RowIter(_ *sql.Context) (sql.RowIter, error) {
	return w.iter, nil
}

type rowIterWrapper struct {
	next  func() (sql.Row, error)
	close func() error
}

var _ sql.RowIter = rowIterWrapper{}

func (w rowIterWrapper) Next() (sql.Row, error) {
	return w.next()
}

func (w rowIterWrapper) Close() error {
	return w.close()
}

func (nd *sortNodeDiffer) makeFromNode() sql.Node {
	return sqlNodeWrapper{
		Node: nd.fromChild,
		iter: rowIterWrapper{
			next: func() (row sql.Row, err error) {
				return nd.nextFromRow()
			},
			close: func() error {
				return nil
			},
		},
	}
}

func (nd *sortNodeDiffer) makeToNode() sql.Node {
	return sqlNodeWrapper{
		Node: nd.toChild,
		iter: rowIterWrapper{
			next: func() (row sql.Row, err error) {
				return nd.nextToRow()
			},
			close: func() error {
				return nd.close()
			},
		},
	}
}

func (nd *sortNodeDiffer) close() error {
	nd.fromIter.close()
	nd.toIter.close()
	return nd.ae.Get()
}
