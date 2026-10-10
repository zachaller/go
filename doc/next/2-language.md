## Changes to the language {#language}

### Union types (experimental) {#union-types}

<!-- go.dev/issue/76920 -->

With `GOEXPERIMENT=uniontypes`, the language supports _union types_
(also known as sum types or tagged unions). A value of a union type
holds exactly one of a fixed set of _variants_:

```go
type Direction union {
	North, South, East, West atom
}

type Maybe[T any] union {
	Unset atom
	Set   T
}
```

The new predeclared type `atom` is an alias for `struct{}` and is used for
variants that carry no data. The zero value of a union is the zero value of
its first variant.

- `T.V` is a value of union type `T` with the atom variant `V`.
- `T{V: x}` is a value of union type `T` with variant `V` holding `x`.
- `u.V = x` sets the (addressable) union `u` to variant `V` with value `x`.
- For an atom variant `V`, `u.V` is an untyped boolean reporting whether
  `V` is the current variant of `u`.
- For other variants, `v, ok := u.V` yields the value of variant `V` and
  whether it is the current variant; `u.V` cannot be used as a single value.
- A union switch `switch v := u.(union) { case V: ... }` selects on the
  current variant. In a clause with a single non-atom variant, `v` has the
  variant's type. Union switches must be exhaustive unless they have a
  `default` clause.

Union types are comparable if all their variant types are comparable.

