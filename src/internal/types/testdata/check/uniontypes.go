// -goexperiment=uniontypes

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Union (sum) types (go.dev/issue/76920).

package uniontypes

// The predeclared type atom is only available if GOEXPERIMENT=uniontypes
// is set when the type checker is initialized; use a local alias.
type atom = struct{}

type Direction union {
	North, South, East, West atom
}

type Maybe[T any] union {
	Unset atom
	Set   T
}

type IP union {
	IPv4 [4]byte
	IPv6 [16]byte
}

type _ union {
	_ int /* ERROR "blank union variant must have type atom" */
}

type _ union /* ERROR "union type must have at least one variant" */ {}

type _ union {
	A atom
	A /* ERROR "A redeclared" */ int
}

type NotCmp union {
	F func()
}

type Rec /* ERROR "invalid recursive type" */ union {
	Self Rec
}

type M union {
	X atom
}

func (M) X /* ERROR "union variant and method with the same name X" */ () {}

func (d Direction) String() string {
	switch d.(union) {
	case North:
		return "N"
	case South:
		return "S"
	case East:
		return "E"
	case West:
		return "W"
	}
	panic(0)
}

func (m Maybe[T]) Get() (T, bool) {
	v, ok := m.Set
	return v, ok
}

func (m *Maybe[T]) Clear() { m.Unset = atom{} }

func _() {
	var d Direction = Direction.North
	var b bool = d.South
	_ = b
	type myBool bool
	var mb myBool = d.East
	_ = mb
	_ = d == Direction.West

	var m Maybe[int]
	m.Set = 42
	v, ok := m.Set
	var _ int = v
	var _ bool = ok
	var x any
	x, mb = m.Set
	_ = x
	_ = Maybe[string]{Set: "s"}
	_ = Maybe[string]{}
	_ = &Maybe[int]{Unset: atom{}}

	ip := IP{IPv4: [4]byte{127, 0, 0, 1}}
	switch v := ip.(union) {
	case IPv4:
		var _ [4]byte = v
	case IPv6:
		var _ [16]byte = v
	}
	switch v := ip.(union) {
	case IPv4, IPv6:
		var _ IP = v
	}
	p := &ip
	switch v := p.(union) {
	default:
		var _ IP = v
	}
	p.IPv6 = [16]byte{}
	_ = map[Direction]int{Direction.North: 1}
}

func _(d Direction, m Maybe[int], e struct{ X Maybe[int] }, i int) {
	_ = m /* ERROR "must be used in a comma-ok assignment" */ .Set
	_ = Maybe[int].Set /* ERROR "cannot evaluate non-atom union variant" */
	_ = Maybe[int]{Unset: atom{}, Set /* ERROR "too many elements in union literal" */ : 1}
	_ = Maybe[int]{1 /* ERROR "union literal elements must be keyed with a variant name" */}
	_ = Maybe[int]{Z /* ERROR "unknown variant Z" */ : 1}
	_ = Maybe[int]{Set: "s" /* ERROR "cannot use" */}
	m.Set = "s" /* ERROR "cannot use" */
	var x int
	m /* ERROR "cannot assign to union variant m.Set in multiple assignment" */ .Set, x = 1, 2
	_ = x
	mm := map[int]Maybe[int]{}
	mm /* ERROR "cannot assign to union variant" */ [0].Set = 1
	_ = NotCmp /* ERROR "cannot be compared" */ {} == NotCmp{}
	_ = d /* ERROR "use of .(union) outside union switch" */ .(union)

	switch /* ERROR "union switch on Direction is not exhaustive: missing South, East, West" */ d.(union) {
	case North:
	}
	switch d.(union) {
	case North, North /* ERROR "duplicate case North in union switch" */:
	case Q /* ERROR "Q is not a variant of Direction" */ :
	default:
	}
	switch v /* ERROR "v declared and not used" */ := m.(union) {
	case Set:
	default:
	}
	switch i /* ERROR "is not a union value" */ .(union) {
	}
}
