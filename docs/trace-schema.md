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
| `started_at`, `finished_at` | UTC |
| `cmoa_version` | the binary that produced it |

The process exit code is 0 when the status is `pass`, 1 when the verifier
answered no (`fail`, `apply_failed`, `timeout`), 2 on a usage or task error
(nothing is printed on stdout), 3 on `runner_error`.

## select.json

| field | meaning |
| --- | --- |
| `rule` | `first` |
| `order` | candidate ids in the order they were considered (configured order) |
| `selection.kind` | `selected` (`candidate_id`, `reason`), `no_candidate` (`tried`), `verifier_failed` (`error`), `judge_timeout` (`after_ms`, chat face only) |
| `also_passed` | other candidates that passed; every candidate is verified even after the first pass, so the layer above can measure how often proposers agree |
| `max_parallel`, `finished_at` | |
