// Copyright 2019 Liquidata, Inc.
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
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2016 Attic Labs, Inc. All rights reserved.
// Licensed under the Apache License, version 2.0:
// http://www.apache.org/licenses/LICENSE-2.0

package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapIterator(t *testing.T) {
	assert := assert.New(t)

	vrw := newTestValueStore()

	me := NewMap(context.Background(), vrw).Edit()
	for i := 0; i < 5; i++ {
		me.Set(String(string(byte(65+i))), Float(i))
	}

	m := me.Map(context.Background())
	test := func(it MapIterator, start int, msg string) {
		for i := start; i < 5; i++ {
			k, v := it.Next(context.Background())
			assert.True(k.Equals(k), msg)
			assert.True(v.Equals(v), msg)
			assert.True(String(string(byte(65+i))).Equals(k), msg)
			assert.True(Float(i).Equals(v), msg)
		}
		k, v := it.Next(context.Background())
		assert.Nil(k, msg)
		assert.Nil(v, msg)
	}

	test(m.Iterator(context.Background()), 0, "Iterator()")
	test(m.IteratorAt(context.Background(), 0), 0, "IteratorAt(0)")
	test(m.IteratorAt(context.Background(), 5), 5, "IteratorAt(5)")
	test(m.IteratorAt(context.Background(), 6), 5, "IteratorAt(6)")
	test(m.IteratorFrom(context.Background(), String("?")), 0, "IteratorFrom(?)")
	test(m.IteratorFrom(context.Background(), String("A")), 0, "IteratorFrom(A)")
	test(m.IteratorFrom(context.Background(), String("C")), 2, "IteratorFrom(C)")
	test(m.IteratorFrom(context.Background(), String("E")), 4, "IteratorFrom(E)")
	test(m.IteratorFrom(context.Background(), String("F")), 5, "IteratorFrom(F)")
	test(m.IteratorFrom(context.Background(), String("G")), 5, "IteratorFrom(G)")
}