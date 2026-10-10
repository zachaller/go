// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package noder

import (
	"go/constant"
	"internal/pkgbits"

	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/syntax"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"cmd/compile/internal/types2"
	"cmd/internal/src"
)

// This file implements the lowering of union (sum) types
// (GOEXPERIMENT=uniontypes, go.dev/issue/76920).
//
// A union type
//
//	union { V0 T0; V1 T1; ... }
//
// is represented in the IR as the struct type
//
//	struct { V0 T0; V1 T1; ...; $tag uintN }
//
// where $tag holds the index of the current variant. Only the field of
// the current variant may hold a non-zero value; all other variant fields
// are always zero. This invariant makes the zero value of the struct the
// zero value of the first variant, and makes struct equality coincide
// with union equality. Since variants are not addressable, the invariant
// is maintained by always constructing a complete new union value when
// setting a variant.
//
// Union operations are lowered as follows:
//
//	T.V            =>  T{$tag: i}
//	T{V: x}        =>  T{$tag: i, V: x}
//	u.V = x        =>  u = T{$tag: i, V: x}
//	u.V            =>  u.$tag == i                    (atom variant V)
//	v, ok = u.V    =>  tmp := u; v, ok = tmp.V, tmp.$tag == i
//	switch v := u.(union) { case V: ... }
//	               =>  tmp := u; switch tmp.$tag { case i: v := tmp.V; ... }

// unionTagName is the name of the tag field in the struct representation
// of a union type.
const unionTagName = types2.SumTagName

// @@@ Writer

// unionSelInfo describes a selector expression x.V that selects
// variant V of a union.
type unionSelInfo struct {
	sel    *syntax.SelectorExpr
	typ    types2.Type // the union type
	sum    *types2.Sum // the underlying union type
	idx    int         // variant index
	isType bool        // x denotes the union type itself (T.V)
	isPtr  bool        // x is a pointer to a union value
}

// unionSel reports whether expr is a union variant selector and
// returns information about it.
func (pw *pkgWriter) unionSel(expr syntax.Expr) (unionSelInfo, bool) {
	sel, ok := syntax.Unparen(expr).(*syntax.SelectorExpr)
	if !ok || isPkgQual(pw.info, sel) {
		return unionSelInfo{}, false
	}
	if _, ok := pw.info.Selections[sel]; ok {
		return unionSelInfo{}, false // field or method selector
	}
	tv, ok := pw.maybeTypeAndValue(sel.X)
	if !ok {
		return unionSelInfo{}, false
	}
	info := unionSelInfo{sel: sel, typ: tv.Type}
	if tv.IsType() {
		info.isType = true
	} else if ptr, ok := tv.Type.Underlying().(*types2.Pointer); ok {
		info.typ = ptr.Elem()
		info.isPtr = true
	}
	info.sum, _ = info.typ.Underlying().(*types2.Sum)
	if info.sum == nil {
		return unionSelInfo{}, false
	}
	info.idx = info.sum.VariantIndex(sel.Sel.Value)
	return info, info.idx >= 0
}

// unionSelExpr writes the expression for a union variant selector
// that is used as a single value.
func (w *writer) unionSelExpr(u unionSelInfo) {
	switch {
	case u.isType:
		// T.V
		w.Code(exprUnionLit)
		w.pos(u.sel)
		w.typ(u.typ)
		w.Bool(false) // not a pointer
		w.Len(u.idx)
		w.Bool(false) // no value
	case u.sum.IsAtom(u.idx):
		// x.V
		w.Code(exprUnionIs)
		w.pos(u.sel)
		w.unionOperand(u)
		w.Len(u.idx)
	default:
		w.p.fatalf(u.sel, "unexpected use of union variant selector %v", syntax.String(u.sel))
	}
}

// unionOperand writes the operand x of a union variant selector x.V.
func (w *writer) unionOperand(u unionSelInfo) {
	w.expr(u.sel.X)
	w.Bool(u.isPtr)
}

// unionCompLit writes the union composite literal lit
// of the (possibly pointer to) union type typ.
func (w *writer) unionCompLit(lit *syntax.CompositeLit, typ types2.Type, isPtr bool) {
	sum := typ.Underlying().(*types2.Sum)
	w.Code(exprUnionLit)
	w.pos(lit)
	w.typ(typ)
	w.Bool(isPtr)
	if len(lit.ElemList) == 0 {
		w.Len(0)
		w.Bool(false)
		return
	}
	kv := lit.ElemList[0].(*syntax.KeyValueExpr)
	idx := sum.VariantIndex(kv.Key.(*syntax.Name).Value)
	w.Len(idx)
	w.Bool(true)
	w.implicitConvExpr(sum.Variant(idx).Type(), kv.Value)
}

// unionAssign writes the assignment u.V = rhs, if lhs is a union
// variant selector u.V. It reports whether it did so.
func (w *writer) unionAssign(pos poser, lhs, rhs syntax.Expr) bool {
	u, ok := w.p.unionSel(lhs)
	if !ok || u.isType {
		return false
	}

	w.Code(stmtAssign)
	w.pos(pos)

	// As if w.assignList(u.sel.X) or w.assignList(*u.sel.X).
	w.Len(1)
	w.Code(assignExpr)
	if u.isPtr {
		w.Code(exprUnaryOp)
		w.op(ir.ODEREF)
		w.pos(u.sel)
	}
	w.expr(u.sel.X)

	// As if w.multiExpr(T{V: rhs}).
	w.Sync(pkgbits.SyncMultiExpr)
	w.Bool(false) // N:N assignment
	w.Len(1)
	w.Code(exprUnionLit)
	w.pos(u.sel)
	w.typ(u.typ)
	w.Bool(false) // not a pointer
	w.Len(u.idx)
	w.Bool(true)
	w.implicitConvExpr(u.sum.Variant(u.idx).Type(), rhs)
	return true
}

// unionCommaOk writes the right-hand side of the assignment v, ok = u.V,
// if expr is a union variant selector u.V. It reports whether it did so.
// It must be called by multiExpr.
func (w *writer) unionCommaOk(dstType func(int) types2.Type, expr syntax.Expr) bool {
	u, ok := w.p.unionSel(expr)
	if !ok || u.isType || u.sum.IsAtom(u.idx) {
		return false
	}

	w.Bool(false) // N:N assignment
	w.Len(2)
	srcs := [2]types2.Type{u.sum.Variant(u.idx).Type(), types2.Typ[types2.Bool]}
	for i, src := range srcs {
		if dst := dstType(i); dst != nil && !types2.Identical(src, dst) {
			// As if w.implicitConvExpr(dst, ...).
			w.Code(exprConvert)
			w.Bool(true) // implicit
			w.typ(dst)
			w.pos(u.sel)
			w.convRTTI(src, dst)
			w.Bool(isTypeParam(dst))
			w.Bool(false) // not identical
		}
		if i == 0 {
			w.Code(exprUnionGet)
			w.pos(u.sel)
			w.unionOperand(u)
		} else {
			w.Code(exprUnionGetOk)
			w.pos(u.sel)
		}
		w.Len(u.idx)
	}
	return true
}

// unionSwitchStmt writes the union switch statement stmt.
func (w *writer) unionSwitchStmt(stmt *syntax.SwitchStmt, guard *syntax.TypeSwitchGuard) {
	w.Sync(pkgbits.SyncSwitchStmt)

	w.openScope(stmt.Pos())
	w.pos(stmt)
	w.stmt(stmt.Init)

	xtyp := w.p.typeOf(guard.X)
	isPtr := false
	if ptr, ok := xtyp.Underlying().(*types2.Pointer); ok {
		xtyp = ptr.Elem()
		isPtr = true
	}
	sum := xtyp.Underlying().(*types2.Sum)

	w.pos(guard)
	if tag := guard.Lhs; w.Bool(tag != nil) {
		w.pos(tag)

		// Like w.localIdent, but we don't have a types2.Object.
		w.Sync(pkgbits.SyncLocalIdent)
		w.pkg(w.p.curpkg)
		w.String(tag.Value)
	}
	w.expr(guard.X)
	w.Bool(isPtr)

	w.Len(len(stmt.Body))
	for i, clause := range stmt.Body {
		if i > 0 {
			w.closeScope(clause.Pos())
		}
		w.openScope(clause.Pos())

		w.pos(clause)

		cases := syntax.UnpackListExpr(clause.Cases)
		w.Len(len(cases))
		for _, cas := range cases {
			w.Len(sum.VariantIndex(cas.(*syntax.Name).Value))
		}

		if obj, ok := w.p.info.Implicits[clause]; w.Bool(ok) {
			pos := clause.Pos()
			if len(cases) != 0 {
				pos = typeExprEndPos(cases[len(cases)-1])
			}
			w.pos(pos)

			obj := obj.(*types2.Var)
			w.typ(obj.Type())
			w.Bool(len(cases) == 1) // variable holds variant value (vs. union value)
			w.addLocal(obj)
		}

		w.stmts(clause.Body)
	}
	if len(stmt.Body) > 0 {
		w.closeScope(stmt.Rbrace)
	}

	w.closeScope(stmt.Rbrace)
}

// sumType writes the union type typ.
func (w *writer) sumType(typ *types2.Sum) {
	w.pkg(typ.Variant(0).Pkg()) // package of tag field
	w.Len(typ.NumVariants())
	for i := 0; i < typ.NumVariants(); i++ {
		v := typ.Variant(i)
		w.pos(v)
		w.selector(v)
		w.typ(v.Type())
	}
}

// @@@ Reader

// sumType reads a union type and returns its struct representation.
func (r *reader) sumType() *types.Type {
	tagPkg := r.pkg()
	n := r.Len()
	fields := make([]*types.Field, n+1)
	for i := 0; i < n; i++ {
		fields[i] = types.NewField(r.pos(), r.selector(), r.typ())
	}
	var tagType *types.Type
	switch types2.SumTagKind(n) {
	case types2.Uint8:
		tagType = types.Types[types.TUINT8]
	case types2.Uint16:
		tagType = types.Types[types.TUINT16]
	default:
		tagType = types.Types[types.TUINT32]
	}
	fields[n] = types.NewField(src.NoXPos, tagPkg.Lookup(unionTagName), tagType)
	return types.NewStruct(fields)
}

// unionTagField returns the tag field of the struct type representing a union.
func unionTagField(typ *types.Type) *types.Field {
	return typ.Field(typ.NumFields() - 1)
}

// unionTag returns the expression x.$tag.
func (r *reader) unionTag(pos src.XPos, x ir.Node) ir.Node {
	return typecheck.Expr(r.curfn, ir.NewSelectorExpr(pos, ir.ODOT, x, unionTagField(x.Type()).Sym))
}

// unionVariant returns the expression x.V for the variant with index idx.
func (r *reader) unionVariant(pos src.XPos, x ir.Node, idx int) ir.Node {
	return typecheck.Expr(r.curfn, ir.NewSelectorExpr(pos, ir.ODOT, x, x.Type().Field(idx).Sym))
}

// unionTagConst returns the tag value for the variant with index idx
// of the union represented by typ.
func unionTagConst(pos src.XPos, typ *types.Type, idx int) ir.Node {
	return ir.NewBasicLit(pos, unionTagField(typ).Type, constant.MakeInt64(int64(idx)))
}

// unionIs returns the expression x.$tag == idx.
func (r *reader) unionIs(pos src.XPos, x ir.Node, idx int) ir.Node {
	return typecheck.Expr(r.curfn, ir.NewBinaryExpr(pos, ir.OEQ, r.unionTag(pos, x), unionTagConst(pos, x.Type(), idx)))
}

// unionOperand reads the operand x of a union variant selector x.V,
// dereferencing it if necessary.
func (r *reader) unionOperand(pos src.XPos) ir.Node {
	x := r.expr()
	if r.Bool() {
		x = typecheck.Expr(r.curfn, ir.NewStarExpr(pos, x))
	}
	return x
}

// unionExpr reads a union expression with the given code.
func (r *reader) unionExpr(code codeExpr) ir.Node {
	switch code {
	case exprUnionLit:
		pos := r.pos()
		typ := r.typ()
		isPtr := r.Bool()
		idx := r.Len()
		var elems []ir.Node
		if r.Bool() {
			elems = append(elems, ir.NewStructKeyExpr(pos, typ.Field(idx), r.expr()))
		}
		elems = append(elems, ir.NewStructKeyExpr(pos, unionTagField(typ), unionTagConst(pos, typ, idx)))
		lit := typecheck.Expr(r.curfn, ir.NewCompLitExpr(pos, ir.OCOMPLIT, typ, elems))
		if isPtr {
			lit = typecheck.Expr(r.curfn, typecheck.NodAddrAt(r.curfn, pos, lit))
			lit.SetType(types.NewPtr(typ))
		}
		return lit

	case exprUnionIs:
		pos := r.pos()
		x := r.unionOperand(pos)
		return r.unionIs(pos, x, r.Len())

	case exprUnionGet:
		pos := r.pos()
		x := r.unionOperand(pos)
		idx := r.Len()
		var init ir.Nodes
		tmp := r.tempCopy(pos, x, &init)
		r.unionTmp = tmp
		return ir.InitExpr(init, r.unionVariant(pos, tmp, idx))

	case exprUnionGetOk:
		pos := r.pos()
		idx := r.Len()
		tmp := r.unionTmp
		base.AssertfAt(tmp != nil, pos, "missing union value for comma-ok expression")
		r.unionTmp = nil
		return r.unionIs(pos, tmp, idx)
	}
	panic("unreachable")
}

// unionSwitchStmt reads a union switch statement.
func (r *reader) unionSwitchStmt(label *types.Sym) ir.Node {
	r.Sync(pkgbits.SyncSwitchStmt)

	r.openScope()
	pos := r.pos()
	var init ir.Nodes
	if n := r.stmt(); n != nil {
		init.Append(n)
	}

	gpos := r.pos()
	var ident *ir.Ident
	if r.Bool() {
		ident = ir.NewIdent(r.pos(), r.localIdent())
	}
	tmp := r.tempCopy(gpos, r.unionOperand(gpos), &init)
	tag := r.unionTag(gpos, tmp)

	clauses := make([]*ir.CaseClause, r.Len())
	for i := range clauses {
		if i > 0 {
			r.closeScope()
		}
		r.openScope()

		pos := r.pos()
		var cases []ir.Node
		var idxs []int
		for range r.Len() {
			idx := r.Len()
			idxs = append(idxs, idx)
			cases = append(cases, unionTagConst(pos, tmp.Type(), idx))
		}
		clause := ir.NewCaseStmt(pos, cases, nil)

		var body []ir.Node
		if r.Bool() {
			name := r.curfn.NewLocal(r.pos(), ident.Sym(), r.typ())
			isVariant := r.Bool()
			r.addLocal(name)

			var val ir.Node = tmp
			if isVariant {
				val = r.unionVariant(pos, tmp, idxs[0])
			}
			as := ir.NewAssignStmt(pos, name, val)
			as.Def = true
			name.Defn = as
			as.PtrInit().Append(ir.NewDecl(pos, ir.ODCL, name))
			body = append(body, typecheck.Stmt(r.curfn, as))
		}

		clause.Body = append(body, r.stmts()...)
		clauses[i] = clause
	}
	if len(clauses) > 0 {
		r.closeScope()
	}
	r.closeScope()

	n := ir.NewSwitchStmt(pos, tag, clauses)
	n.Label = label
	n.SetInit(init)
	return n
}
