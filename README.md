# CMoA

Architecture decisions live in `docs/adr` and are gated by
[DocDag](https://github.com/Kaikei-e/DocDag) v0.3.0.

## Decision records

Records are Markdown with YAML frontmatter (`NNNN-kebab-title.md`). Typed
edges — `supersedes`, `depends-on` — are declared in frontmatter. Body
wikilinks such as `[[0001]]` must name a real record.

```sh
# next free identifier, official template
docdag new "Title of the decision"

# replace an existing record (rewrites its status in the same pass)
docdag new "Replacement title" --supersedes 0001
```

Ask the graph instead of reading the directory:

```sh
docdag query --binding                          # what is in force
docdag resolve 0001                             # what replaced this
docdag context 0001                             # the record and its neighbourhood
docdag validate                                 # invariants; exits 1 on error
docdag validate --touching docs/adr/0001-*.md   # findings one edit can break
docdag lint                                     # the rules in docdag.yaml
```

## Install

```sh
go install github.com/Kaikei-e/DocDag/cmd/docdag@v0.3.0
```

This installs `docdag` into `$(go env GOPATH)/bin`. `docdag.yaml` pins the
`adr` preset and the corpus directory; CI downloads the v0.3.0 release
binary via `Kaikei-e/DocDag@v0.3.0`. Locally, `pre-commit install` runs the
same checks on Markdown and `docdag.yaml` edits.
