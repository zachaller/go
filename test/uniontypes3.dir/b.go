// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "./a"

type myErr string

func (e myErr) Error() string { return string(e) }

var global = a.Shape{Circle: 1}
var gv, gok = global.Circle

func check(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func main() {
	check(a.Area(a.NewCircle(2)) == 12, "circle")
	check(a.Area(a.Shape{Rect: a.Rect{W: 2, H: 5}}) == 10, "rect")
	check(a.Area(a.Shape.Empty) == 0, "empty")
	check(a.IsEmpty(a.Shape.Empty) && !a.IsEmpty(global), "IsEmpty")
	check(gv == 1 && gok, "globals")

	var s a.Shape
	s.Rect = a.Rect{W: 1, H: 1}
	switch v := s.(union) {
	case Rect:
		check(v.W == 1, "Rect")
	default:
		panic("default")
	}

	r := a.Try(func() (int, error) { return 12, nil })
	check(r.Must() == 12, "Try")
	r2 := a.Try(func() (int, error) { return 0, myErr("boom") })
	err, ok := r2.Err
	check(ok && err.Error() == "boom", "Err")

	n, w := a.Count(a.Tokens())
	check(n == 1 && w == 1, "Count")
}
