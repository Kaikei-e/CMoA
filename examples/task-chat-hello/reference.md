Yes, in one place that people meet in practice: encoding/json. A nil slice
marshals to `null`, an empty slice marshals to `[]`. A client that does
`for (const x of body.items)` breaks on `null` and not on `[]`, so the
difference reaches the wire and then the other program.

Almost everywhere else the two behave the same: `len`, `cap`, `range` and
`append` all work on a nil slice, and `s == nil` is the only ordinary way
to tell them apart. Comparing `len(s) == 0` treats them alike, which is
why it is the usual test.
