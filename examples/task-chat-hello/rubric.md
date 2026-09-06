- Answers the question actually asked: whether the difference is
  observable, with one concrete case.
- The case is real and checkable. `encoding/json` marshalling `nil` as
  `null` and an empty slice as `[]` is the canonical one; another correct
  case is fine.
- Says what is *not* different, so the reader does not over-correct:
  `len`, `cap`, `range` and `append` all work on a nil slice.
- Does not invent behaviour Go does not have.
- Length, formatting and tone are not quality.
