// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package a

type Shape union {
	Circle float64
	Rect   Rect
	Empty  atom
}

type Rect struct{ W, H float64 }

func Area(s Shape) float64 {
	switch v := s.(union) {
	case Circle:
		return 3 * v * v
	case Rect:
		return v.W * v.H
	case Empty:
		return 0
	}
	panic("unreachable")
}

func IsEmpty(s Shape) bool { return s.Empty }

func NewCircle(r float64) Shape { return Shape{Circle: r} }

type Result[T any] union {
	Ok  T
	Err error
}

func Try[T any](f func() (T, error)) Result[T] {
	v, err := f()
	if err != nil {
		return Result[T]{Err: err}
	}
	return Result[T]{Ok: v}
}

func (r Result[T]) Must() T {
	switch v := r.(union) {
	case Ok:
		return v
	case Err:
		panic(v)
	}
	panic("unreachable")
}

type token union {
	num  int
	word string
}

func Tokens() []any {
	return []any{token{num: 1}, token{word: "x"}}
}

func Count(xs []any) (n, w int) {
	for _, x := range xs {
		switch t := x.(token); t.(union) {
		case num:
			n++
		case word:
			w++
		}
	}
	return
}
