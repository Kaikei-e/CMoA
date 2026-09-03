# CMoA

**Common Mixture-of-Agents.** A general-purpose Mixture-of-Agents runtime
for both chat and coding, built on what the 2025–2026 evidence on MoA
supports: heterogeneous proposers, a single judge, selection rather than
synthesis.

CMoA is the middle of a three-layer stack:

| Layer | Project | What it guarantees |
| --- | --- | --- |
| Ground | [DocDag](https://github.com/Kaikei-e/DocDag) | Markdown + YAML frontmatter read as a typed graph; declared relations are consistent |
| Frame | **CMoA** | The runtime below: routing, aggregation, traces, and the surfaces a harness may edit |
| Fit-out | [uzushio](https://github.com/Kaikei-e/uzushio) | The specification, the conformance tests, and the lineage of harness edits |

CMoA depends on DocDag through read-only commands only. It does not depend
on uzushio; uzushio depends on it.

## Status

**Pre-implementation.** No runtime code exists yet. What this repository
holds today is the architecture decision records under `docs/adr/`, gated
by DocDag so that a superseded decision cannot keep saying it is in force.
The first milestone is the coding face alone: N proposers, selection by
verifier pass, no judge model to calibrate.

## Scope

CMoA owns exactly four things. Anything else belongs to a layer above or
below it.

1. **A deterministic router and proposer pool.** Which proposers run is
   decided by configuration, never by asking a model.
2. **Selection-type aggregation.** On the coding face a candidate is
   selected by passing the verifier; on the chat face by a single judge.
   Candidates are never merged into one answer.
3. **Traces as files.** Every run writes its candidates, the reason for the
   selection, the models and resources used, and the `as_of` day and `at`
   revision of the specification it read, so the run can be reconstructed
   later with `docdag --as-of <day> --at <rev> query --binding`.
4. **A declaration of editable surfaces.** Of the harness components a
   self-improvement loop may touch (system prompt, tool descriptions,
   skills, middleware, sub-agent config, memory), which are open. The
   verifier, the tracer and the model configuration are read-only.

## Decision records

Records are Markdown with YAML frontmatter under `docs/adr/`
(`NNNN-kebab-title.md`). Typed edges — `supersedes`, `depends-on` — are
declared in frontmatter. Body wikilinks such as `[[0001]]` must name a real
record.

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
same checks on Markdown and `docdag.yaml` edits; the hook builds `docdag`
from source and needs a Go toolchain.

## Contributing

Issues and pull requests are welcome. A decision is changed by superseding
it, not by editing it: add a new record with `--supersedes`, and let
`docdag validate` confirm the lineage before you open the pull request.

## License

No license file has been added yet. Until one lands, the code and documents
here are not yet licensed for reuse.
