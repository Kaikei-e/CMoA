# Architecture decision records

Thirteen records stand behind what CMoA is. They are the reasoning; the code under `internal/` and the
trace schema in [../trace-schema.md](../trace-schema.md) are what the binary does, so a record is
read for *why* a flag, a status name or a file exists, and the code for what it accepts today.
Records 0002 onward are written in Japanese, the language they were argued in.

Read **0011 first, then 0013**: 0011 fixes the scope of v1 — the coding face of v0 plus a chat face
with a single blind judge, six commands of which only `serve` stays resident, Go 1.27 and the standard
library alone — and carries forward the type vocabulary the other records lean on. It supersedes 0009,
which fixed v0 at three commands and no judge; 0009 in turn superseded 0002. 0013 supersedes 0011 and
changes exactly one of its decisions: how the chat face turns six pairwise verdicts into one answer.
Everything else 0011 decided is carried forward word for word, so 0011 is still the record to read for
what v1 *is*, and 0013 for how it now chooses. All three stay as the records the others were argued
against.
0003 through 0008 each take one of the four responsibilities CMoA owns and settle it; they depend on
0002 and, where noted, on each other, and can otherwise be read in any order.

Every record is **Accepted** except 0002, which 0009 superseded on 2026-09-05, 0009, which 0011 superseded on 2026-09-06,
and 0011, which 0013 superseded the same day. A decision that
replaces one of these declares `supersedes:` in its frontmatter and moves the old record's status to
`superseded`; nobody edits an accepted record to change what it decided. `docdag validate` is the
gate that keeps that true. Record numbers in frontmatter are **quoted** (`supersedes: ["0011"]`):
unquoted, YAML reads `0011` as an octal integer and the edge silently lands on 0009.

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

## [0009 — a third command, `verify`, and `task.json` version 2](0009-add-verify-command-and-task-v2.md) — superseded by 0011

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

## [0010 — `propose` reads a rendered harness directory](0010-harness-directory.md)

The record that closes the gap 0007 left open: binding edits were listed in `run.json` but never
reached a proposer. uzushio now renders the binding set into a directory — `system-prompt.md`
appended after the fixed template, `memory/**/*.md` as a Notes section, `skills/<name>/SKILL.md` as a
name-and-description list — and `cmoa propose --harness <dir>` reads it, hashing the tree it read
into `harness.render` so the layer above can check it rendered what it meant to. `--seed` and
`--temperature` pin every proposer for a run, which is what a paired baseline-versus-edit measurement
needs. CMoA still interprets no vault content; it reads files.

## [0011 — v1: the chat face, a blind pairwise judge, `judge` and `serve`](0011-chat-face-blind-pairwise-judge-and-serve.md) — superseded by 0013

The record that opens the second face. A chat task is `task.json` version 3 with `face: chat`; the same
router asks every proposer, and a single judge model of a different family picks one answer by
round-robin pairwise comparison, both orders per pair, a win only when the orders agree, the Condorcet
winner selected and everything else an honest `NoCandidate` with its reason. The judge is blind — no
proposer names, lengths or timings reach it — candidates sit inside nonce-delimited blocks, and the
verdict is one JSON object with the reason before the choice. `cmoa judge` runs the same protocol over
externally supplied answers so the layer above can calibrate the judge against human labels; `cmoa
serve` puts the face behind OpenAI-compatible HTTP and answers a `NoCandidate` with a 502 rather than
a guess. It supersedes 0009 and keeps everything 0009 decided about the coding face.

## [0012 — the monitor observes, and asks for a round only as a client of `serve`](0012-monitor-observes-traces-and-servers.md)

The record that puts a screen on a round without adding a seventh command. CMoA Monitor is a
separate SvelteKit process under `monitor/`; it reads the write-once trace directory and each
server's read-only `/slots`, derives the same lane, pair-grid, selection and timeline states the
terminal watcher showed, and pushes them over server-sent events. It never runs `propose` or
`select` itself and writes nothing: its chat panel relays a conversation to `cmoa serve` exactly as
any other client would, so `serve` runs the round and writes the trace, and the monitor pins that
run on screen. A `no_candidate` is shown as the fault it is, never patched over by a human pick or
a retry. Configuration is `cmoa.json` plus the directories to watch, never a second list of
proposers. Depends on 0007 and 0011.

## [0013 — consensus first, then a Copeland score](0013-consensus-then-copeland-for-chat-selection.md)

The record that fixes what 0011 got wrong about draws, and nothing else. The chat face asked for a
Condorcet winner over six pairwise calls and treated every draw as no information, so ten of eleven
served requests and 217 of 400 calibration runs came back `no_candidate` — including a question all
three proposers answered correctly and identically, and one where the two right answers each beat the
third and then tied each other. Agreement among candidates was being read as failure. So the face now
compares the answers to each other first: normalise them (a hand-rolled compatibility fold, case,
markdown, list markers, whitespace, trailing punctuation), and if a strict majority say the same
thing — the same text, or the same last number in a short answer — return one of them and never ask
the judge. When they disagree the six calls run unchanged and the draws are finally counted: a win is
1, a draw the judge answered is 0.5 to each side, which is MT-Bench's own reading of an inconsistent
swap and the way an arena folds a tie into Bradley-Terry. A machine failure still outranks the score.
What the score cannot part goes to one recorded chain — agreement with the rest of the run, then the
shorter answer where the gap is real, then the lowest SHA-256 of the answer's own text, and for
answers identical to the byte the lowest candidate id under its own name — and no key in it reads the
presentation position or the proposer order, because 0011's argument against those is right and is
kept. `outcome.kind` does not move, so the calibration above keeps counting; `cycle`,
`no_majority` and `all_draws` stay in the vocabulary as words older traces carry, and are no longer
produced. It supersedes 0011 and keeps every other decision 0011 made.
