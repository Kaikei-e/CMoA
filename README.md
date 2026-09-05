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

**Two faces.** On the **coding face** `cmoa propose` asks every configured
proposer for a unified diff, and `cmoa select` applies each diff to its own
git worktree, runs the task's verifier in a container, and selects the first
candidate in configured order that passes. `cmoa verify` runs that same
verification for one diff named on the command line, so the verifier itself
can be measured.

On the **chat face** `cmoa propose` sends the task's conversation to the
same pool and keeps each answer, and `cmoa select` puts the answers to a
single judge model, pairwise and in both orders. `cmoa judge` runs that
protocol over answers produced somewhere else, and `cmoa serve` puts the
whole face behind an OpenAI-compatible endpoint on loopback.

There is still no dependency outside the Go standard library. What comes
next, and in what order, is in [docs/roadmap.md](docs/roadmap.md).

```sh
go build -o bin/cmoa ./cmd/cmoa
cd examples/task-hello && ./setup.sh
cmoa propose --task . --config /path/to/cmoa.json     # writes runs/<run-id>/
cmoa propose --task . --harness ./render --seed 7 --temperature 0
cmoa select  --task .                                  # verifies, writes select.json
cmoa verify  --task . --diff reference.diff            # one diff, one JSON object
cmoa surfaces                                          # the editable surfaces
```

```sh
cd examples/task-chat-hello
cmoa propose --task . --config /path/to/cmoa.json     # every proposer answers the conversation
cmoa select  --task .                                  # asks the judge, writes judge.json
cmoa judge   --task . --candidate a.txt --candidate b.txt --candidate c.txt
cmoa serve   --config /path/to/cmoa.json               # POST /v1/chat/completions
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

`--harness <dir>` names a *rendered harness directory* — the tree a layer
above materialises from the harness edits that are in force. CMoA reads
three things out of it: `system-prompt.md` is appended to its own output
contract (never replacing it), `memory/**/*.md` become a `## Notes` section
in path order, and each `skills/<name>/SKILL.md` contributes one
`- <name>: <description>` line. Skill bodies are not rendered; CMoA has no
step that would load one. On the chat face all three reach the single
system message, because the rest of the prompt is the task's conversation
and CMoA does not edit a turn. The directory is per run, so it is a flag
and not a `cmoa.json` field, and an empty one renders the prompt a run
without it renders, byte for byte. CMoA hashes the tree itself and records every file
and the digest in `run.json` as `harness.render`, so what a renderer says
it wrote and what CMoA read can be compared.

The harness is counted against the task's own `max_context_bytes`: a note
is as much of the model's context as a file is, so a tree that does not fit
refuses the run (exit 3) instead of overrunning the server's context and
being scored as a regression. A harness that would make an edit measure as
a no-op for the wrong reason is refused too — a skill directory with no
`SKILL.md`, a skill with no description, a name outside
`^[a-z0-9][a-z0-9._-]{0,63}$`, a file that is not valid UTF-8 or not a
regular file. `--harness ""` is an error, not "no harness".

`--seed <int>` and `--temperature <float>` override *every* proposer's seed
and temperature for one run, and the effective config in `run.json` records
the values that were sent. They are independent flags, so pairing them is
the caller's job: a repeated measurement wants both — `--seed <n>
--temperature 0` — because a seed alone still samples.

## The chat face

`task.json` version 3 adds a `face`. A version 3 coding task carries exactly
the version 2 fields; a chat task carries a `conversation.json` instead of a
repository — a JSON array of `{role, content}` ending with a `user` message
— and, optionally, a `reference.answer` and a `rubric`. Those last two are
shown to the **judge only**: a proposer handed the reference answer is not
answering the question. `cmoa.json` version 2 adds a `judge` block naming
the judge endpoint, and a `serve` block; version 1 files keep their meaning
and mean "no judge, no serve". See `examples/task-chat-hello`.

Selection on the chat face is **round-robin pairwise with an order swap**.
Three answers make three pairs, each asked in both orders: six calls. A pair
is won only when both orders name the same candidate; a disagreement, or a
`tie` in either order, is a draw and scores nothing for either side. A
candidate that wins every pair it appears in is the Condorcet winner and is
selected.

Anything else is `no_candidate`, with a sub-reason — `cycle`,
`no_majority`, `all_draws`, `invalid_output` or `too_few_candidates`. There
is no re-ask beyond one retry for malformed JSON, and **no deterministic
fallback**: "take the first" or "take the shorter" would reinstate as a rule
exactly the position and length biases the order swap exists to detect.
`no_candidate` is an outcome CMoA already has a word for, and the layer
above decides whether to send the question to a person or drop it. The
distribution of the sub-reasons over a calibration set is itself a measure
of the judge.

`all_draws` is a coarse union — a judge that abstained, one that
contradicted itself under swap, and one that was never reached all land in
it — so every pair also records **why** it drew: `tie`, `disagree`,
`invalid` or `unmeasured`, counted in `judge.json` as `draw_reasons`. Three
different findings about a judge reported under one word is the conflation
an agreement metric must not make, so the split is kept where a calibration
can read it.

A pair nobody could answer does not throw away a winner it could not have
unseated: if one candidate has already beaten every other, a timeout in the
pair between two losers leaves the selection standing and is recorded as
`unmeasured`. Only when the missing answers could still decide the outcome
does it become `judge_timeout` or `judge_failed`.

The judge is asked blind. The candidates are labelled `A` and `B` inside a
call and mapped back only in the trace. Candidate text is fenced with a
nonce; invisible characters (C0 controls, zero-width runes) are dropped
first and anything still resembling the closing tag is escaped after, so a
control character hidden inside the tag cannot survive the escape and then
be tidied into a working one. Injection-shaped phrases are flagged —
**recorded, never acted on**: silently dropping a flagged candidate would be
a second, unmeasured judge. Everything the judge saw is on disk, in
`judge/<pair>-<ab|ba>.json` and `judge.json`.

There is no presentation permutation: both orders of every pair are always
asked, so shuffling the candidates cannot change one byte the judge reads.
The nonce is what a re-run varies, and it is derived from a seed — `--seed`,
or the run id — rather than drawn afresh. That makes `--seed` a real
perturbation of the prompt, the same question in different irrelevant bytes
whose answer ought not to change, and it makes a selection reproducible from
its own trace.

`cmoa judge` performs the same protocol over answers CMoA did not produce,
which is what a calibration needs:

```sh
cmoa judge --task <chat task> --candidate c1.txt --candidate c2.txt --candidate c3.txt --seed 7
```

`--seed` changes only the nonce; `--judge-seed` changes the judge's own
sampling seed. Both `select` and `judge` print one JSON object on the chat
face and exit 0 whatever the outcome, and both refuse a run that has
already been judged *before* making a call, so an interrupted attempt
cannot be made to spend the fleet twice.

`cmoa serve` answers `GET /v1/models` and `POST /v1/chat/completions`. Every
request becomes a task directory and a full run trace under `serve.runs_dir`,
so an answer served over HTTP is as reconstructible as one produced by the
CLI. A 200 carries a `cmoa` extension field with the run id, the selection,
the judge's call count and swap consistency, and the harness digest — but
not the id of the proposer whose answer won, which stays in the trace. A
selection that did not happen is an error, not a 200 with an apology:
`no_candidate` is 502 with the sub-reason as `error.code`, a judge that
could not be asked is 502, and one that ran out of time is 504. `stream:
true` returns the wire format as a single chunk; the judge cannot compare
answers that do not exist yet, so there is nothing to stream. The server
binds loopback and has no auth, so a non-loopback address needs
`--allow-remote`.

## Scope

CMoA owns exactly four things. Anything else belongs to a layer above or
below it.

1. **A deterministic router and proposer pool.** Which proposers run is
   decided by configuration, never by asking a model.
2. **Selection-type aggregation.** On the coding face a candidate is
   selected by passing the verifier; on the chat face by a single judge,
   pairwise and in both orders. Candidates are never merged into one
   answer, and the judge never writes an answer of its own.
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

CMoA needs Go 1.27 and git at runtime, and Docker Compose for the coding
face (`select` runs `docker compose run`); the chat face runs no container.
`make test` runs the unit tests without a model, Docker or DocDag;
`CMOA_E2E=1 CMOA_CONFIG=... make e2e` runs `examples/task-hello` and
`examples/task-chat-hello` against a live fleet.

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
