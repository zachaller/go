// errorcheck -goexperiment uniontypes

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test errors for union (sum) types (go.dev/issue/76920).

package p

type D union {
	A, B atom
	C    int
}

type E union {
	_    atom
	X, Y atom
}

type _ union {
	_ int // ERROR "blank union variant must have type atom"
}

type _ union{} // ERROR "union type must have at least one variant"

type _ union {
	A atom
	A int // ERROR "A redeclared"
}

type NotCmp union {
	F func()
}

type Rec union { // ERROR "invalid recursive type"
	Self Rec
}

type M union {
	X atom
}

func (M) X() {} // ERROR "union variant and method with the same name X"

func f() {
	var d D
	_ = d.C                     // ERROR "union variant selector d.C must be used in a comma-ok assignment"
	_ = D.C                     // ERROR "cannot evaluate non-atom union variant D.C"
	_ = D{A: atom{}, B: atom{}} // ERROR "too many elements in union literal"
	_ = D{1}                    // ERROR "union literal elements must be keyed with a variant name"
	_ = D{Z: 1}                 // ERROR "unknown variant Z"
	_ = D{C: "s"}               // ERROR "cannot use .s."
	d.C = "s"                   // ERROR "cannot use .s."
	var x int
	d.C, x = 1, 2 // ERROR "cannot assign to union variant d.C in multiple assignment"
	_ = x
	m := map[int]D{}
	m[0].C = 1               // ERROR "cannot assign to union variant m\[0\].C"
	_ = d.C + 1              // ERROR "must be used in a comma-ok assignment"
	_ = &d.C                 // ERROR "must be used in a comma-ok assignment"
	d.C++                    // ERROR "must be used in a comma-ok assignment"
	_ = NotCmp{} == NotCmp{} // ERROR "cannot be compared"
}

func g(d D, e E, i int) {
	switch d.(union) { // ERROR "union switch on D is not exhaustive: missing B, C"
	case A:
	}
	switch d.(union) {
	case A, A: // ERROR "duplicate case A"
	case Q: // ERROR "Q is not a variant of D"
	default:
	}
	switch v := d.(union) { // ERROR "v declared and not used"
	case C:
	default:
	}
	switch e.(union) { // ERROR "missing _"
	case X, Y:
	}
	switch i.(union) { // ERROR "is not a union value"
	}
	switch d.(union) {
	case A:
		fallthrough // ERROR "fallthrough"
	case B, C:
	}
}
