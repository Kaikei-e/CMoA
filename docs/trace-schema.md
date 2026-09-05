# Trace schema (v1)

CMoA writes one directory per run and never reads it back, except that
`select` reads the candidates `propose` left in the same run. uzushio and
people read the rest. The Go types are in `internal/trace/trace.go`; this
page is the contract for readers outside the module.

```
<task>/runs/<run-id>/
  run.json                        written once by propose
  prompt/<proposer-id>.json       the exact request sent
  candidates/<proposer-id>.json   what came back, with a status
  candidates/<proposer-id>.raw.txt
  candidates/<proposer-id>.diff   only when status is ok
  verify/<proposer-id>/result.json  written by select
  verify/<proposer-id>/stdout.txt
  verify/<proposer-id>/stderr.txt
  select.json                     written once by select
```

`run-id` is `YYYYMMDDTHHMMSSZ-<8 hex>` in UTC, so the lexicographically
largest directory is the latest run. Every JSON file is written atomically.
`run.json` and `select.json` are write-once.

## run.json

| field | meaning |
| --- | --- |
| `schema_version` | 1 |
| `run_id`, `created_at`, `cmoa_version`, `prompt_version` | identity of the run and of the code and prompt templates that produced it |
| `task` | `id`, `dir`, `repo`, `rev` (as written), `resolved_rev` (commit SHA), `files`, `instruction_sha256` |
| `config` | the effective `cmoa.json` after defaults; holds `api_key_env` names, never key values |
| `harness` | `vault`, `as_of` (valid time, YYYY-MM-DD), `at` (vault commit, `-dirty` suffix when the tree had changes), `docdag_version`, `binding` (id/title/status/path of each binding document), `render` (the rendered harness directory, absent when the run was given none) |
| `proposers` | `id`, `model`, `base_url` per proposer, in configured order |
| `byzantine` | `n` proposers, `f = floor((n-1)/3)` deceptive proposers tolerated |

With `harness.as_of` and `harness.at`, `docdag --as-of <as_of> --at <at>
query --binding` in the vault reconstructs what the run read.

### harness.render

`cmoa propose --harness <dir>` is given a *rendered harness directory*: the
tree a layer above materialises from the harness edits that are in force.
CMoA reads it, renders it into the prompt, and records what it read.

```json
"render": {
  "dir": "/absolute/path/to/render",
  "tree_sha256": "9f2c…",
  "rendered_bytes": 357,
  "files": [
    {"path": "memory/00-conventions.md", "sha256": "…"},
    {"path": "skills/emit-diff/SKILL.md", "sha256": "…"},
    {"path": "system-prompt.md", "sha256": "…"}
  ]
}
```

`dir` is absolute, as `harness.vault` is, so two runs naming the same
directory differently record the same value.

`files` lists **every** file in the tree in path order, by its
slash-separated path relative to `dir`, whether or not CMoA renders it — a
file on a surface CMoA cannot inject yet still makes a distinct harness
state. Two paths are excluded: `.git` (a directory, or the one-line file a
worktree or submodule leaves) and `.git/**`, which is not harness content,
and a top-level `render.json`, which is the renderer's own manifest and
cannot contain its own digest.

`rendered_bytes` is how much of the harness reached the two messages: the
system appendix, every note body, and one name-plus-description per skill.
It is the number the context budget below is spent on, and it lets a reader
correlate a bad run with prompt length.

`tree_sha256` is sha256 over the concatenation of `<path>\n<sha256>\n` for
each entry of `files`, in that order. The whole tree is one number, so a
renderer's claim about what it wrote and CMoA's record of what it read
compare in one comparison. **The digest is computed by CMoA from the
directory it read**; a manifest the renderer supplies is never copied into
the trace.

The digest is over *files*. An empty directory does not reach it, so two
trees that differ only by one cannot be told apart by the comparison — which
is why an empty `skills/<name>/` is refused outright (below) rather than
left to it.

`prompt_version` digests the prompt *templates*. It is `harness.render`
that says what was poured into them, so the two together identify the
prompt a run sent.

### the harness directory

| path | rendered as |
| --- | --- |
| `system-prompt.md` | appended to the system message after CMoA's own contract, verbatim, under a `HARNESS` heading. The contract comes first and is never replaced |
| `memory/**/*.md` | a `## Notes` section of the user message: every `.md` file under `memory/`, in path order, each as its body. A file that is not `.md` (a `.gitkeep`) and a body that is only whitespace are not notes |
| `skills/<name>/SKILL.md` | a `## Available skills` section of the user message, one `- <name>: <description>` line per skill, in path order. The **body is not rendered**: CMoA v0 has no step at which a skill could be invoked, so rendering a body would model a harness that does not exist |

`<description>` is the frontmatter `description:` when there is one, and
otherwise the first line of the body that is neither blank nor a heading;
newlines in it are folded to spaces. A skill's name is its directory name
and must match `^[a-z0-9][a-z0-9._-]{0,63}$` — the listing is one line per
skill, and a name a machine proposed must not be able to write lines of its
own.

Everything else about the bytes is the renderer's: content is rendered
verbatim, CRLF line endings included.

Four things are refused, with exit 3 — each of them would otherwise make an
edit measure as a no-op for the wrong reason, or make the digest commit to
bytes that were never sent:

- a `skills/<name>/` with no `SKILL.md` **file**, empty directory included;
- a `SKILL.md` with no description at all, or a skill name outside the
  alphabet above;
- a file that is not valid UTF-8 (`… is not valid UTF-8; proposers only see
  text`), which the JSON encoder would silently replace on the way out, and
  anything that is not a regular file (a symlink is refused, never
  followed);
- a tree whose rendered bytes do not fit the budget below.

A directory holding none of the three surfaces renders exactly the prompt a
run without `--harness` renders, byte for byte: an edit that adds nothing
measures as nothing. That rendering is pinned by a golden in
`internal/prompt/testdata/`.

### the context budget

`task.json`'s `max_context_bytes` (default 65536) bounds the instruction and
the files. The harness is counted against the **same** budget: a Notes
section is as much of the model's context as a file is, and `memory` and
`skills` are the auto-accepted surfaces, so nothing human-gated stands
between a mined pattern and an unbounded Notes section.

`instruction + files + rendered_bytes > max_context_bytes` refuses the run
with exit 3, before anything is written, and the message names both numbers:

```
cmoa: propose: context budget exceeded: instruction and files 1180 bytes
plus harness 101 bytes total 1281, over max_context_bytes 1200
```

Without `--harness` the check is the one `task.json` already made.

## candidates/<id>.json

| field | meaning |
| --- | --- |
| `status` | `ok` (a diff was extracted), `http_error`, `timeout`, `malformed` (2xx but not a chat completion), `no_diff` (a completion with no unified diff in it) |
| `error` | the error text for anything but `ok` |
| `finish_reason`, `usage.prompt_tokens`, `usage.completion_tokens` | as the server reported |
| `timings.request_ms` | measured by CMoA; `server_prompt_ms`, `server_predicted_ms`, `tokens_per_second` come from llama-server's `timings` and are absent on other servers |
| `diff` | `files`, `additions`, `deletions`, `sha256` of the `.diff` file; only when `status` is `ok` |
| `request_sha256`, `response_sha256` | digests of the exact HTTP bodies |
| `started_at`, `finished_at` | UTC |

## verify/<id>/result.json

| field | meaning |
| --- | --- |
| `status` | `pass` (exit 0), `fail`, `apply_failed` (the diff did not apply), `timeout`, `runner_error` (docker itself failed), `skipped` (candidate status was not `ok`) |
| `exit_code`, `duration_ms`, `command`, `project_name` | the compose invocation |
| `apply_error`, `error` | text when relevant |

## verify (single verification)

`cmoa verify --task <dir> --diff <file>` verifies one diff outside any run.
It prints exactly one JSON object on stdout, and with `--out <dir>` writes
the same object as `<dir>/result.json` alongside `stdout.txt` and
`stderr.txt` (the container's output). It writes nothing under `runs/`.
`result.json` is write-once: a verification directory records one
verification. The directory is created and checked before the verifier runs;
if the result still cannot be written afterwards, the status becomes
`runner_error` and the JSON object is printed all the same.

| field | meaning |
| --- | --- |
| `schema_version` | 1 |
| `task` | the task id |
| `rev` | the commit SHA the worktree was made at |
| `diff_sha256` | of the diff bytes as read, so a caller can pin what was verified |
| `label` | `--label`, or 8 hex digits; it names the compose project `cmoa-<task>-verify-<label>`, so a label must match `^[a-z0-9][a-z0-9_-]{0,63}$` and one that does not is refused rather than rewritten |
| `status` | as in `verify/<id>/result.json` above, minus `skipped`: `pass`, `fail`, `apply_failed`, `timeout`, `runner_error` |
| `exit_code`, `duration_ms`, `command`, `project_name` | the compose invocation |
| `apply_error`, `error` | text when relevant; absent when empty. Without `--out`, a `runner_error` folds the tail of the container's stderr into `error` |
| `band` | only for a `verify.kind: band` task; see below. Absent for an `exit-code` verifier |
| `started_at`, `finished_at` | UTC |
| `cmoa_version` | the binary that produced it |

The process exit code is 0 when the status is `pass`, 1 when the verifier
answered no (`fail`, `apply_failed`, `timeout`), 2 on a usage or task error
(nothing is printed on stdout), 3 on `runner_error`.

The diff named by `--diff` may be an empty file. It verifies the revision
unchanged, which is how a task's seed state is shown to fail — and, when the
task's `reference.diff` is itself empty, how a reference solution that *is*
the tree at `rev` is verified.

### band verifiers

A task whose `task.json` sets `verify.kind: band` is judged on what the
container printed, not on its exit code. Such a verifier measures
invariants — a latency, a throughput, an allocation count — and each has a
band it must fall inside; an exit code cannot say which one moved.

The contract is one CSV block on stdout whose header line is exactly

```
invariant,value,ci_half,band_lo,band_hi,verdict
```

followed by one row per invariant. `verdict` is `pass`, `fail`, `skipped` or
`info`; the four numbers may be empty, which a `skipped` or `info` row
normally leaves them. Every other line on stdout is the container's own
logging and is ignored, so a gate need not keep its output clean. The block
ends at the first line that is not a six-field CSV record. If several blocks
appear, the **last** one is read: a gate that re-measures prints the run that
counts last.

| status | when |
| --- | --- |
| `fail` | at least one row has `verdict: fail`, whatever the container exited |
| `pass` | rows were read and none failed, and the container exited 0. `skipped` rows do not withhold a pass |
| `runner_error` | no header line, a header with no rows under it, or a row that does not parse (`error`: "band verifier printed no gate CSV" / "... a malformed gate CSV"); or every band held and the container still exited non-zero (`error` carries the code) — the harness broke, which says nothing about the code under test |
| `apply_failed`, `timeout` | as for an `exit-code` verifier |

`exit_code` stays the container's in every case.

```json
"band": {
  "judged": 1,
  "failed": [],
  "skipped": ["tail_alloc_bytes"],
  "rows": [
    {"invariant": "p99_latency_ms", "value": 12.5, "ci_half": 0.4, "band_lo": 0, "band_hi": 15, "verdict": "pass"},
    {"invariant": "tail_alloc_bytes", "value": null, "ci_half": null, "band_lo": null, "band_hi": null, "verdict": "skipped"}
  ]
}
```

`judged` counts the rows a band was actually applied to (`pass` plus
`fail`); `failed` and `skipped` name those rows; `rows` keeps every row in
the order it was printed, `info` rows included. A number the verifier left
empty is `null`, which is "not measured" — not a measurement of zero.

`cmoa select` refuses a band task with exit 3: a banded gate measures, and
CMoA has no way to propose candidates against a measurement yet. Band tasks
are verified one diff at a time, by the layer above.

## select.json

| field | meaning |
| --- | --- |
| `rule` | `first` |
| `order` | candidate ids in the order they were considered (configured order) |
| `selection.kind` | `selected` (`candidate_id`, `reason`), `no_candidate` (`tried`), `verifier_failed` (`error`), `judge_timeout` (`after_ms`, chat face only) |
| `also_passed` | other candidates that passed; every candidate is verified even after the first pass, so the layer above can measure how often proposers agree |
| `max_parallel`, `finished_at` | |
