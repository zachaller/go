// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"go/ast"
	"go/token"
	"internal/buildcfg"
	. "internal/types/errors"
)

// This file implements union (sum) types as described in
// go.dev/issue/76920:
//
//	type Direction union {
//		North, South, East, West atom
//	}
//
//	type Maybe[T any] union {
//		Unset atom
//		Set   T
//	}
//
// A value of a union type holds exactly one of its variants. The zero
// value of a union is the zero value of its first variant.
//
// Union types are called "sum types" in this package to avoid confusion
// with the type set unions (*Union) used in constraint interfaces.

// ----------------------------------------------------------------------------
// API

// A Sum represents a union (sum) type.
type Sum struct {
	variants []*Var // variants != nil indicates the union is set up
}

// NewSum returns a new union type with the given variants.
// Variants are represented as fields; non-blank variant names must be unique.
func NewSum(variants []*Var) *Sum {
	var vset objset
	for _, v := range variants {
		if v.name != "_" && vset.insert(v) != nil {
			panic("multiple variants with the same name")
		}
	}
	s := &Sum{variants: variants}
	s.markComplete()
	return s
}

// NumVariants returns the number of variants in the union (including blank variants).
func (s *Sum) NumVariants() int { return len(s.variants) }

// Variant returns the i'th variant for 0 <= i < NumVariants().
func (s *Sum) Variant(i int) *Var { return s.variants[i] }

// VariantIndex returns the index of the variant with the given name,
// or -1 if there is no such variant. Blank variants are never found.
func (s *Sum) VariantIndex(name string) int {
	if name == "_" {
		return -1
	}
	for i, v := range s.variants {
		if v.name == name {
			return i
		}
	}
	return -1
}

// lookupVariant returns the index of the variant with the given name
// that is accessible from package pkg, or -1 if there is no such variant.
// Unexported variants are only accessible from their own package.
func (s *Sum) lookupVariant(pkg *Package, name string) int {
	if name == "_" {
		return -1
	}
	for i, v := range s.variants {
		if v.sameId(pkg, name, false) {
			return i
		}
	}
	return -1
}

// IsAtom reports whether the i'th variant has type atom (struct{}),
// i.e., whether it carries no data.
func (s *Sum) IsAtom(i int) bool { return isAtom(s.variants[i].typ) }

func (s *Sum) Underlying() Type { return s }
func (s *Sum) String() string   { return TypeString(s, nil) }

// isAtom reports whether t is identical to struct{}.
func isAtom(t Type) bool {
	s, _ := Unalias(t).(*Struct)
	return s != nil && len(s.fields) == 0
}

// asSum returns the union type underlying t, or nil.
func asSum(t Type) *Sum {
	if t == nil {
		return nil
	}
	s, _ := t.Underlying().(*Sum)
	return s
}

// ----------------------------------------------------------------------------
// Implementation

func (s *Sum) markComplete() {
	if s.variants == nil {
		s.variants = make([]*Var, 0)
	}
}

func (check *Checker) unionTypeExpr(styp *Sum, e *ast.UnionType) {
	if !buildcfg.Experiment.UnionTypes {
		check.error(e, UnsupportedFeature, "union types require GOEXPERIMENT=uniontypes")
	}

	var variants []*Var
	var vset objset
	n := 0
	for _, f := range e.Variants.List {
		typ := check.varType(f.Type)
		if len(f.Names) == 0 {
			check.error(f.Type, InvalidSyntaxTree, "union variant must have a name")
			continue
		}
		for _, ident := range f.Names {
			n++
			name := ident.Name
			v := NewField(ident.Pos(), check.pkg, name, typ, false)
			if name == "_" {
				if isValid(typ) && !isAtom(typ) {
					check.error(f.Type, InvalidBlank, "blank union variant must have type atom")
				}
				variants = append(variants, v)
				check.recordDef(ident, v)
				continue
			}
			if check.declareInSet(&vset, ident.Pos(), v) {
				variants = append(variants, v)
				check.recordDef(ident, v)
			}
		}
	}
	if n == 0 {
		check.error(e, InvalidUnion, "union type must have at least one variant")
	}
	if len(variants) > 1<<16 {
		check.error(e, InvalidUnion, "too many union variants")
	}

	styp.variants = variants
	styp.markComplete()
}

// unionSelector handles the selector expression e (x.sel) if x denotes a
// union type or a (pointer to a) union value and sel names one of its
// variants. It reports whether e was handled; in that case x is updated
// to hold the result.
//
//   - T.V, where V is an atom variant, is a value of type T with variant V.
//   - v.V, where V is an atom variant, is an untyped boolean reporting
//     whether V is the current variant of v.
//   - v.V, where V is not an atom variant, is a two-valued expression
//     (value, ok) that must be used in a comma-ok assignment.
//   - On the left-hand side of an assignment, v.V = x sets v to variant V
//     with value x; in that case v must be addressable.
func (check *Checker) unionSelector(x *operand, e *ast.SelectorExpr) bool {
	sel := e.Sel.Name

	if x.mode() == typexpr {
		s := asSum(x.typ())
		if s == nil {
			return false
		}
		i := s.lookupVariant(check.pkg, sel)
		if i < 0 {
			return false // may be a method expression
		}
		if !s.IsAtom(i) {
			check.errorf(e.Sel, NotAnExpr, "cannot evaluate non-atom union variant %s.%s; use a composite literal %s{%s: value}", x.typ(), sel, x.typ(), sel)
			x.invalidate()
			return true
		}
		check.recordUse(e.Sel, s.variants[i])
		x.mode_ = value
		// x.typ_ is the union type
		x.expr = e
		return true
	}

	typ := x.typ()
	isPtr := false
	if p, _ := typ.Underlying().(*Pointer); p != nil {
		typ = p.base
		isPtr = true
	}
	s := asSum(typ)
	if s == nil {
		return false
	}
	i := s.lookupVariant(check.pkg, sel)
	if i < 0 {
		return false
	}
	v := s.variants[i]
	check.recordUse(e.Sel, v)

	if check.unionLHS == e {
		// assignment v.V = x
		if !check.unionAssignOk {
			check.errorf(e, UnassignableOperand, "cannot assign to union variant %s in multiple assignment", ExprString(e))
			x.invalidate()
			return true
		}
		if x.mode() != variable && !isPtr {
			check.errorf(e, UnassignableOperand, "cannot assign to union variant %s (union value is not addressable)", ExprString(e))
			x.invalidate()
			return true
		}
		x.mode_ = variable
		x.typ_ = v.typ
		x.expr = e
		return true
	}

	if s.IsAtom(i) {
		x.mode_ = value
		x.typ_ = Typ[UntypedBool]
	} else {
		x.mode_ = variantok
		x.typ_ = v.typ
	}
	x.expr = e
	return true
}

// unionSwitchStmt type-checks a union switch:
//
//	switch [v :=] x.(union) {
//	case V1: ...
//	case V2, V3: ...
//	default: ...
//	}
//
// In a clause with exactly one non-atom variant V, v has the type of V
// and holds the variant's value. In clauses with multiple variants and
// in the default clause, v has the union type. In a clause with a single
// atom variant, v is not declared.
//
// A union switch must be exhaustive unless it has a default clause.
func (check *Checker) unionSwitchStmt(inner stmtContext, s *ast.UnionSwitchStmt) {
	// A union switch is like a type switch; reuse its scoping rules.
	lhs := s.Lhs
	if lhs != nil {
		if lhs.Name == "_" {
			// _ := x.(union) is an invalid short variable declaration
			check.softErrorf(lhs, NoNewVar, "no new variable on left side of :=")
			lhs = nil // avoid declared and not used error below
		} else {
			check.recordDef(lhs, nil) // lhs variable is implicitly declared in each cause clause
		}
	}

	var x operand
	check.expr(nil, &x, s.X)
	if !x.isValid() {
		return
	}

	utyp := x.typ()
	if p, _ := utyp.Underlying().(*Pointer); p != nil {
		utyp = p.base
	}
	sum := asSum(utyp)
	if sum == nil {
		check.errorf(&x, InvalidTypeSwitch, "%s is not a union value", &x)
		return
	}

	check.multipleDefaults(s.Body.List)

	covered := make([]bool, len(sum.variants))
	hasDefault := false
	var lhsVars []*Var // list of implicitly declared lhs variables
	for _, s := range s.Body.List {
		clause, _ := s.(*ast.CaseClause)
		if clause == nil {
			check.error(s, InvalidSyntaxTree, "incorrect union switch case")
			continue
		}

		var variant *Var // the single variant of this clause, if any
		if clause.List == nil {
			hasDefault = true
		}
		for _, c := range clause.List {
			name, _ := c.(*ast.Ident)
			if name == nil {
				check.error(c, InvalidExprSwitch, "union switch case must be a variant name")
				continue
			}
			j := sum.lookupVariant(check.pkg, name.Name)
			if j < 0 {
				check.errorf(name, MissingFieldOrMethod, "%s is not a variant of %s", name.Name, utyp)
				continue
			}
			if covered[j] {
				check.errorf(name, DuplicateCase, "duplicate case %s in union switch", name.Name)
			}
			covered[j] = true
			check.recordUse(name, sum.variants[j])
			if len(clause.List) == 1 {
				variant = sum.variants[j]
			}
		}

		check.openScope(clause, "case")
		// If lhs exists, declare a corresponding variable in the case-local scope.
		if lhs != nil {
			var T Type
			switch {
			case variant == nil:
				T = utyp
			case !isAtom(variant.typ):
				T = variant.typ
			}
			if T != nil {
				obj := newVar(LocalVar, lhs.Pos(), check.pkg, lhs.Name, T)
				check.declare(check.scope, nil, obj, clause.Colon)
				check.recordImplicit(clause, obj)
				// For the "declared and not used" error, all lhs variables act as
				// one; i.e., if any one of them is 'used', all of them are 'used'.
				// Collect them for later analysis.
				lhsVars = append(lhsVars, obj)
			}
		}
		check.stmtList(inner, clause.Body)
		check.closeScope()
	}

	if !hasDefault {
		var missing []string
		for j, ok := range covered {
			if !ok {
				v := sum.variants[j]
				name := v.name
				switch {
				case name == "_":
					name = "_ (blank variant; add a default case)"
				case !v.Exported() && v.pkg != check.pkg:
					name += " (unexported variant; add a default case)"
				}
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			check.softErrorf(s, InvalidExprSwitch, "union switch on %s is not exhaustive: missing %s", utyp, joinNames(missing))
		}
	}

	// If lhs exists, we must have at least one lhs variable that was used.
	if lhs != nil {
		used := len(lhsVars) == 0
		for _, v := range lhsVars {
			if check.usedVars[v] {
				used = true
			}
			check.usedVars[v] = true // avoid usage error when checking entire function
		}
		if !used {
			check.softErrorf(lhs, UnusedVar, "%s declared and not used", lhs.Name)
		}
	}
}

// isUnionGuard reports whether e is a union switch guard x.(union).
func isUnionGuard(e *ast.TypeAssertExpr) bool {
	id, ok := e.Type.(*ast.Ident)
	return ok && id.Name == "union"
}

func joinNames(list []string) string {
	s := ""
	for i, n := range list {
		if i > 0 {
			s += ", "
		}
		s += n
	}
	return s
}

// sumTagKind returns the kind of the (hidden) tag that identifies the
// current variant of a union with n variants.
func sumTagKind(n int) BasicKind {
	switch {
	case n <= 1<<8:
		return Uint8
	case n <= 1<<16:
		return Uint16
	}
	return Uint32
}

// sumTagName is the name of the hidden tag field in the struct
// representation of a union type. It is not a valid identifier
// and thus cannot collide with a variant name.
const sumTagName = "$tag"

// layout returns the struct type used to represent s in memory:
// one field per variant followed by a tag identifying the current
// variant. Only the field of the current variant may be non-zero.
// (The tag comes last so that unions of only atom variants do not
// need padding for a trailing zero-sized field.)
// This must match the representation used by the compiler backend.
func (s *Sum) layout() *Struct {
	fields := make([]*Var, 0, len(s.variants)+1)
	fields = append(fields, s.variants...)
	fields = append(fields, NewField(token.NoPos, nil, sumTagName, Typ[sumTagKind(len(s.variants))], false))
	return &Struct{fields: fields}
}

// emptyStruct is the type struct{}, the underlying type of the
// predeclared type atom.
var emptyStruct = Struct{fields: []*Var{}}
