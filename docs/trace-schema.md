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
| `harness` | `vault`, `as_of` (valid time, YYYY-MM-DD), `at` (vault commit, `-dirty` suffix when the tree had changes), `docdag_version`, `binding` (id/title/status/path of each binding document) |
| `proposers` | `id`, `model`, `base_url` per proposer, in configured order |
| `byzantine` | `n` proposers, `f = floor((n-1)/3)` deceptive proposers tolerated |

With `harness.as_of` and `harness.at`, `docdag --as-of <as_of> --at <at>
query --binding` in the vault reconstructs what the run read.

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
