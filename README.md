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

**v0, coding face.** `cmoa propose` asks every configured proposer for a
unified diff; `cmoa select` applies each diff to its own git worktree,
runs the task's verifier in a container, and selects the first candidate
in configured order that passes. `cmoa verify` runs that same verification
for one diff named on the command line, so the verifier itself can be
measured. There is no judge model, no daemon and no dependency outside the
Go standard library. What comes next, and in what order, is in
[docs/roadmap.md](docs/roadmap.md).

```sh
go build -o bin/cmoa ./cmd/cmoa
cd examples/task-hello && ./setup.sh
cmoa propose --task . --config /path/to/cmoa.json     # writes runs/<run-id>/
cmoa select  --task .                                  # verifies, writes select.json
cmoa verify  --task . --diff reference.diff            # one diff, one JSON object
cmoa surfaces                                          # the editable surfaces
```

`cmoa.json` names the proposers (any OpenAI-compatible `/v1/chat/completions`
endpoint, such as `llama-server`), the DocDag vault the run reads, and the
verifier's parallelism and timeout. A task is a directory holding
`task.json`, `instruction.md` and a `compose.yaml` with a `verify` service;
see `examples/task-hello`. `task.json` version 2 adds the task's own
reference solution and a set of mutants, so a layer above can ask how good
the verifier is; version 1 files keep their meaning. The reference diff may
be an empty file, which says the tree at `rev` already is the reference
solution. Version 2 also chooses how the verifier answers: `verify.kind:
exit-code` (the default) reads the container's exit status, while `band`
reads a CSV of measured invariants and their bands off its stdout, so a
performance gate can say *which* invariant moved. `cmoa verify` judges both
kinds; `cmoa select` judges exit-code verifiers only. What a run leaves
behind, and the band CSV's contract, are in
[docs/trace-schema.md](docs/trace-schema.md).

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
go install github.com/Kaikei-e/CMoA/cmd/cmoa@latest
go install github.com/Kaikei-e/DocDag/cmd/docdag@v0.3.0   # propose reads the vault through it
```

CMoA needs Go 1.27, git and Docker Compose at runtime (`select` runs
`docker compose run`). `make test` runs the unit tests without a model,
Docker or DocDag; `CMOA_E2E=1 CMOA_CONFIG=... make e2e` runs
`examples/task-hello` against a live proposer fleet.

`docdag.yaml` pins the
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
