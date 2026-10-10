// run -goexperiment uniontypes

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test union (sum) types (go.dev/issue/76920).

package main

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

type Direction union {
	North, South, East, West atom
}

func (d Direction) String() string {
	switch d.(union) {
	case North:
		return "North"
	case South:
		return "South"
	case East:
		return "East"
	case West:
		return "West"
	}
	panic("unreachable")
}

func (d *Direction) TurnRight() {
	switch d.(union) {
	case North:
		*d = Direction.East
	case East:
		*d = Direction.South
	case South:
		*d = Direction.West
	case West:
		*d = Direction.North
	}
}

type Maybe[T any] union {
	Unset atom
	Set   T
}

func Some[T any](v T) Maybe[T] { return Maybe[T]{Set: v} }

func (m Maybe[T]) Get() (T, bool) {
	v, ok := m.Set
	return v, ok
}

func (m Maybe[T]) Or(def T) T {
	if v, ok := m.Set; ok {
		return v
	}
	return def
}

func (m *Maybe[T]) Clear() { m.Unset = atom{} }

type IP union {
	IPv4 [4]byte
	IPv6 [16]byte
}

type Expr union {
	Num int
	Add *[2]Expr
	Neg *Expr
}

func eval(e Expr) int {
	switch v := e.(union) {
	case Num:
		return v
	case Add:
		return eval(v[0]) + eval(v[1])
	case Neg:
		return -eval(*v)
	}
	panic("unreachable")
}

type Extendable union {
	_ atom // zero value; forces default cases

	A, B atom
}

type Big union {
	S   string
	P   *int
	E   error
	F   float64
	Arr [3]int
}

type Holder struct {
	name string
	val  Big
}

type Result[T any] union {
	Ok  T
	Err error
}

func try[T any](v T, err error) Result[T] {
	if err != nil {
		return Result[T]{Err: err}
	}
	return Result[T]{Ok: v}
}

func check(cond bool, msg string, args ...any) {
	if !cond {
		panic(fmt.Sprintf(msg, args...))
	}
}

var global = Some("hello")
var gv, gok = global.Set
var gUnset = global.Unset

func main() {
	// Atom variants, zero value, equality.
	var d Direction
	check(d == Direction.North, "zero value must be first variant")
	check(d.North && !d.South, "atom selectors")
	d.TurnRight()
	check(d == Direction.East, "TurnRight: got %v", d)
	d.TurnRight()
	d.TurnRight()
	check(d.String() == "West", "String: got %v", d)
	check(fmt.Sprint(Direction.South) == "South", "fmt")
	d.East = atom{}
	check(d.East, "assign atom variant")

	// Non-atom variants and comma-ok.
	var m Maybe[int]
	_, ok := m.Set
	check(!ok && m.Unset, "zero Maybe")
	m.Set = 42
	v, ok := m.Set
	check(ok && v == 42 && !m.Unset, "set Maybe")
	check(m == Some(42) && m != Some(41) && m != Maybe[int]{}, "Maybe equality")
	m.Clear()
	check(m == Maybe[int]{} && m.Or(7) == 7, "Clear")

	// Existing variables and conversions in comma-ok assignments.
	type myBool bool
	var mb myBool
	var x any
	x, mb = Some(3).Set
	check(x == 3 && mb == true, "comma-ok with conversions")
	if _, ok := Some("x").Unset, true; ok {
		check(true, "")
	}

	// Different shapes for generic unions.
	s := Some("str")
	check(s.Or("") == "str", "Maybe[string]")
	type pt struct{ x, y int }
	p := Some(pt{1, 2})
	if v, ok := p.Get(); !ok || v.y != 2 {
		panic("Maybe[pt]")
	}
	ip := new(int)
	pp := Some(ip)
	if v, _ := pp.Set; v != ip {
		panic("Maybe[*int]")
	}

	// Arrays as variants; switch with variable.
	addr := IP{IPv4: [4]byte{127, 0, 0, 1}}
	switch v := addr.(union) {
	case IPv4:
		check(v[0] == 127, "IPv4")
	case IPv6:
		panic("IPv6")
	}
	addr.IPv6 = [16]byte{15: 1}
	if _, ok := addr.IPv4; ok {
		panic("IPv4 after setting IPv6")
	}
	check(addr == IP{IPv6: [16]byte{15: 1}}, "IP equality")

	// Recursive unions through pointers.
	e := Expr{Add: &[2]Expr{{Num: 3}, {Neg: &Expr{Num: 10}}}}
	check(eval(e) == -7, "eval: got %d", eval(e))

	// Blank variants require default cases; zero value is distinct.
	var ext Extendable
	n := 0
	switch ext.(union) {
	case A, B:
		n = 1
	default:
		n = 2
	}
	check(n == 2 && !ext.A && !ext.B, "Extendable zero value")
	ext = Extendable.B
	switch v := ext.(union) {
	case A, B:
		check(v == Extendable.B, "multi-variant case binds union value")
	default:
		panic("default")
	}

	// Pointer-containing variants (GC), embedding in structs.
	h := []Holder{{"s", Big{S: "abc"}}, {"p", Big{P: new(int)}}, {"e", Big{E: errors.New("boom")}}, {"a", Big{Arr: [3]int{1, 2, 3}}}}
	for _, h := range h {
		switch v := h.val.(union) {
		case S:
			check(h.name == "s" && v == "abc", "S")
		case P:
			check(h.name == "p" && *v == 0, "P")
		case E:
			check(h.name == "e" && v.Error() == "boom", "E")
		case Arr:
			check(h.name == "a" && v[2] == 3, "Arr")
		default:
			panic("default")
		}
	}
	h[0].val.F = 1.5
	if f, ok := h[0].val.F; !ok || f != 1.5 {
		panic("F")
	}
	if _, ok := h[0].val.S; ok {
		panic("S after F")
	}

	// Unions as map keys and in interfaces.
	names := map[Direction]string{Direction.North: "n", Direction.South: "s"}
	check(names[Direction.South] == "s" && names[Direction.West] == "", "map keys")
	var iface any = Some(1.5)
	if mf, ok := iface.(Maybe[float64]); !ok || mf.Or(0) != 1.5 {
		panic("interface")
	}
	check(iface == any(Some(1.5)) && iface != any(Some(2.5)), "interface equality")

	// Generic constructors.
	r := try(1, nil)
	if v, ok := r.Ok; !ok || v != 1 {
		panic("try ok")
	}
	r = try(0, errors.New("bad"))
	if err, ok := r.Err; !ok || err.Error() != "bad" {
		panic("try err")
	}

	// Closures and goroutines.
	var cur Maybe[string]
	set := func(s string) { cur.Set = s }
	set("closure")
	check(cur.Or("") == "closure", "closure")
	ch := make(chan Direction, 1)
	go func() { ch <- Direction.West }()
	check(<-ch == Direction.West, "channel")

	// Switch with init statement and label.
	ds := []Direction{Direction.North, Direction.West, Direction.South}
	count := 0
L:
	for _, dir := range ds {
		switch x := dir; x.(union) {
		case West:
			break L
		default:
			count++
		}
	}
	check(count == 1, "labeled break: got %d", count)

	// Package-level initialization.
	check(gv == "hello" && gok && !gUnset, "globals")

	// Sizes as computed by the type checker must match the runtime.
	check(unsafe.Sizeof(Direction{}) == reflect.TypeFor[Direction]().Size(), "Sizeof Direction")
	check(unsafe.Sizeof(Big{}) == reflect.TypeFor[Big]().Size(), "Sizeof Big")
	check(unsafe.Alignof(IP{}) == uintptr(reflect.TypeFor[IP]().Align()), "Alignof IP")
	check(unsafe.Sizeof(Direction{}) == 1, "Sizeof Direction: got %d", unsafe.Sizeof(Direction{}))
}
