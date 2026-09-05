# Architecture decision records

Nine records stand behind what CMoA is. They are the reasoning; the code under `internal/` and the
trace schema in [../trace-schema.md](../trace-schema.md) are what the binary does, so a record is
read for *why* a flag, a status name or a file exists, and the code for what it accepts today.
Records 0002 onward are written in Japanese, the language they were argued in.

Read **0009 first**: it fixes the scope of v0 — three commands, no daemon, no judge model, Go 1.27 and
the standard library alone — and the type vocabulary the other records lean on. It supersedes 0002,
which said the same with two commands; 0002 stays as the record the others were argued against.
0003 through 0008 each take one of the four responsibilities CMoA owns and settle it; they depend on
0002 and, where noted, on each other, and can otherwise be read in any order.

Every record is **Accepted** except 0002, which 0009 superseded on 2026-09-05. A decision that
replaces one of these declares `supersedes:` in its frontmatter and moves the old record's status to
`superseded`; nobody edits an accepted record to change what it decided. `docdag validate` is the
gate that keeps that true.

## [0001 — adopt DocDag for architecture decision records](0001-adopt-docdag-for-architecture-decision-records.md)

The record that makes the rest checkable. Decisions stay ordinary Markdown under `docs/adr/`, but
their relations — `supersedes`, `depends-on` — are declared in frontmatter and read as a typed
graph, so a superseded record whose status never moved, a cycle in the supersession chain, or a
reference to a file nobody wrote becomes a CI finding rather than a thing a reviewer might notice.
It chooses the `adr` preset over `spec`: CMoA is recording decisions, not publishing a normative
standard of clauses and conformance tests.

## [0002 — the scope of v0: two commands, and the Go standard library](0002-v0-scope-two-commands-and-go-standard-library.md) — superseded by 0009

The record that opens the series and mostly says no. v0 is the coding face alone — `propose` and
`select`, plus `surfaces` and `version`; nothing stays resident, and no judge model is asked
anything, because on this face the selector is a test run and a test run needs no calibration. It
decides Go 1.27.1 with an empty `require` block, and how a language without sum types is made to
carry them: constants with `exhaustive`, sealed interfaces with `gochecksumtype` declared and
switched in one package, `(T, error)` with typed errors and `errors.AsType`, validated newtypes for
anything that comes from outside. It declines cobra, fp-go, Rust, Zig, a resident server, a judge on
the coding face, and generated enum code — every one of them for the same reason, that the code has
to stay ordinary Go an agent can edit.

## [0003 — the proposer pool and the deterministic router](0003-proposer-pool-and-deterministic-router.md)

Who is asked, in what order, and what they are shown — all three settled by configuration, never by
asking a model, which is the arrangement the evidence supports. The backend is one endpoint, an
OpenAI-compatible `POST /v1/chat/completions` spoken by every local server worth pointing at; the
configuration is JSON where an unknown key is an error rather than a silent fall back to a default;
proposers run in file order, one sample each, at their own temperature. It names the three models —
Granite 4.2 8B, Qwen3.5-9B, Ministral 3 8B, separated along lab, lineage, architecture and tokenizer
— and demotes Gemma 4 to spare over an open llama.cpp issue that corrupts its output on this exact
GPU. It records `f = ⌊(n−1)/3⌋` into every run and says plainly that three proposers tolerate none.
And it corrects the pool-selection rule its own sources refute: minimise β, the rate at which every
proposer fails together, not the pairwise error correlation that cannot identify β.

## [0004 — candidates are unified diffs](0004-candidates-are-unified-diffs.md)

"A small model cannot write a unified diff" is true only of bare `git apply`. Measured against git
2.43.0, wrong hunk counts, wrong start lines and drifted context whitespace are all absorbed by
`--recount --ignore-whitespace`; exactly three errors are not, and one of them — a wrongly indented
added line — *applies successfully* and fails the compiler instead. So the record asks the model for
one fenced diff with no `diff --git` lines and a mandatory `@@`, extracts it deterministically,
applies it to a per-candidate `git worktree` in a single pass, and refuses to retry: an HTTP error, a
timeout, a malformed body and a missing diff are all recorded as candidates with a status, because
how often a proposer fails is the measurement. Context is the task's files in full — no tool calling,
no file exploration, no line numbers.

## [0005 — the verifier runs `docker compose run`](0005-verifier-runs-docker-compose-run.md)

CMoA executes code it did not write, so the isolation has to be real and it must not be CMoA's to
maintain. The task ships a compose file; CMoA runs one service from it, once, per candidate, under a
project name unique to that candidate so concurrent runs share no container, network or volume,
hands the worktree over through `CMOA_CANDIDATE_DIR`, and tears the project down with `-v
--remove-orphans` whatever happened. A timeout interrupts docker before it kills it. The record's
sharpest line is a type distinction: docker missing or a compose file unreadable is a
`*verify.RunnerError` and becomes `VerifierFailed`, which says nothing about any candidate, while a
non-zero exit inside the container is that candidate's `fail`. Mixing the two would poison every
statistic the layer above computes.

## [0006 — the rule `first`, and the sealed `Selection`](0006-selection-rule-first-and-selection-sum-type.md)

Among candidates a binary verifier has judged equal, the first in configured order wins — arbitrary,
but stated, and readable afterwards from the `order` field. The record then refuses the obvious
optimisation: every candidate is verified even after one passes, because stopping early leaves the
rest unobserved and the ceiling the pool is measured against is the rate at which *all* of them fail.
The outcome is a sealed four-variant type — `Selected`, `NoCandidate`, `JudgeTimeout` (declared for
the chat face, never produced on this one), `VerifierFailed` — mirrored into `select.json`, and a
run is selected exactly once. `select` exits 0 even when nothing passed: that is a fact about the
run, and calling it a failure is somebody else's job.

## [0007 — traces are files, written once and never read back](0007-run-traces-as-files.md)

One run is one directory under the task, named `YYYYMMDDTHHMMSSZ-<8 hex>` so the newest sorts last
and a person can still read the date. Inside it: what was read to begin (the effective config, and
the DocDag vault with the `as_of` day and `at` revision — `-dirty` appended when the vault had
uncommitted changes, which is the record admitting the run cannot be reconstructed), what was sent,
what came back, what the containers did, and what was selected. Everything is written temp-then-
rename, and `run.json` and `select.json` refuse to overwrite. CMoA never reads a trace back, save for
`select` reading the candidates of its own run. The record also retracts a promise: bit-exact
reproduction is not on offer on this hardware, so a trace guarantees description, not replay.

## [0008 — editable surfaces and autonomy](0008-editable-surfaces-and-autonomy.md)

The root package exports the vocabulary and nothing else: seven surfaces a self-improvement loop may
propose edits for, three autonomy levels, and — in a separate type, so a loop over surfaces cannot
pick them up by accident — the three components it may only read. Memory and skills are auto-accepted
on a held-out pass; the system prompt needs a person, not because prose is frightening but because it
is the one component whose solo edit measurably regressed; tool implementation is propose-only,
being arbitrary code that runs. The whole public API is six functions and three types, so traces
cross to the layer above as JSON rather than as Go types, and the internals stay free to move.
Raising an autonomy level means superseding this record, never editing it — a run's trace has to be
readable against the rules that were in force when it ran.

## [0009 — a third command, `verify`, and `task.json` version 2](0009-add-verify-command-and-task-v2.md)

The record that replaces 0002 and carries all of it forward except the count. uzushio's `task doctor`
has to measure the verifier — does a known-good solution pass it, does it catch injected defects —
and it has to measure the same verifier that `select` uses, not a second implementation of it. So
`cmoa verify --task --diff` runs one diff through the `select` path (worktree, `git apply`, the task's
compose file under a unique project name) and prints one JSON object; unlike `select`, its exit code
follows the result, because the caller is uzushio and the judgement is uzushio's. `task.json` gains a
version 2 with the reference solution, the mutants and the doctor's thresholds; CMoA reads them and
computes nothing from them — the reference diff may even be empty, which says the tree at `rev`
already is the solution. It also gains `verify.kind: band`, for a gate that measures rather than
answers: such a verifier prints a CSV of invariants, values and their bands, and one row outside its
band is the candidate's `fail`, while a container that exits non-zero with every band held is a
`runner_error` — the harness broke, and that is not a fact about the code. `select` refuses a band
task outright; a measurement is not something the pool can be asked to satisfy.
