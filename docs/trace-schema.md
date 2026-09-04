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

## select.json

| field | meaning |
| --- | --- |
| `rule` | `first` |
| `order` | candidate ids in the order they were considered (configured order) |
| `selection.kind` | `selected` (`candidate_id`, `reason`), `no_candidate` (`tried`), `verifier_failed` (`error`), `judge_timeout` (`after_ms`, chat face only) |
| `also_passed` | other candidates that passed; every candidate is verified even after the first pass, so the layer above can measure how often proposers agree |
| `max_parallel`, `finished_at` | |
