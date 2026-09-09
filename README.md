# CMoA

**Common Mixture-of-Agents** is a Go runtime that asks a configured pool of
models for candidates and selects one. It supports chat and coding, preserves
how each decision was made, and never merges candidates or writes a replacement
answer.

| Face | Candidates | Selection |
| --- | --- | --- |
| Coding | Unified diffs | Apply each diff in its own Git worktree, run the task's Docker verifier, select the first passing candidate in configured order |
| Chat | Conversation replies | Select by consensus first; otherwise compare answers with one blind, pairwise judge |

CMoA uses [DocDag](https://github.com/Kaikei-e/DocDag) through read-only commands
to record the specification a run read. [uzushio](https://github.com/Kaikei-e/uzushio)
builds evaluation and harness-improvement workflows on CMoA; CMoA does not depend
on it. See the [roadmap](docs/roadmap.md) for scope and status.

## Build and configure

The Go runtime uses only the standard library. Building requires Go 1.27.1 or
later; runs need Git, DocDag, and running OpenAI-compatible model endpoints.
Coding verification also needs Docker Compose. The optional monitor has its own
Node/SvelteKit dependencies; `cmoa serve` and the monitor can also be started
together with the repository Compose file.

From the repository root:

```sh
go install github.com/Kaikei-e/DocDag/cmd/docdag@v0.4.1
export PATH="$(go env GOPATH)/bin:$PATH"
go build -o bin/cmoa ./cmd/cmoa
```

To install CMoA on your PATH instead, use
`go install github.com/Kaikei-e/CMoA/cmd/cmoa@latest`.

Create `cmoa.json`, replacing the model names, endpoints and vault path with
your own. The vault must be an existing Git-backed DocDag vault; CMoA does not
start model servers.

```json
{
  "version": 2,
  "proposers": [
    {"id": "p1", "base_url": "http://127.0.0.1:8081/v1", "model": "model-a"},
    {"id": "p2", "base_url": "http://127.0.0.1:8082/v1", "model": "model-b"},
    {"id": "p3", "base_url": "http://127.0.0.1:8083/v1", "model": "model-c"}
  ],
  "harness": {"vault": "/path/to/docdag-vault"},
  "judge": {"base_url": "http://127.0.0.1:8090/v1", "model": "judge-model"},
  "serve": {"listen": "127.0.0.1:8095", "pool_name": "cmoa", "runs_dir": "serve-runs"}
}
```

Configuration is discovered through `--config`, then `$CMOA_CONFIG`, then
`<task>/cmoa.json`, then `./cmoa.json`. Relative vault and serve paths resolve
against the configuration file. Version 1 supports coding; version 2 adds the
optional `judge` and `serve` blocks. Chat requires a judge, including when
proposing candidates.

Defaults cover sampling, token limits, timeouts and concurrency. Endpoints can
use `api_key_env` for credentials and `extra_body` for additional request fields.
The judge defaults to `output_format: "json_schema"`; use `"none"` if your
endpoint does not support it. See the [configuration types](internal/config/config.go)
and [validation/defaults](internal/config/validation.go) for the full contract.

## Run a task

These examples use the binary built above and run from the repository root:

```sh
export CMOA_CONFIG="$PWD/cmoa.json"

# Coding: setup recreates the example's generated repository.
sh examples/task-hello/setup.sh
./bin/cmoa propose --task examples/task-hello
./bin/cmoa select --task examples/task-hello
./bin/cmoa verify --task examples/task-hello --diff examples/task-hello/reference.diff

# Chat: generate answers, then select from the latest run.
./bin/cmoa propose --task examples/task-chat-hello
./bin/cmoa select --task examples/task-chat-hello

# Judge existing answer files, or re-aggregate a recorded run without model calls.
./bin/cmoa judge --task examples/task-chat-hello --candidate a.txt --candidate b.txt
./bin/cmoa judge --task examples/task-chat-hello --replay-from /path/to/run

./bin/cmoa surfaces --format json
./bin/cmoa --help
```

`propose` prints the new run directory. `select --run <dir>` selects a specific
run; omitting `--run` uses the latest under the task. Coding selection prints a
text result; chat selection prints JSON. `judge` prints the JSON outcome on its
first line and the new run directory on the next. Both selection commands exit
0 for a recorded outcome, so inspect `kind` to determine whether anything was
selected. `verify` emits JSON. Each command supports `--help`.

| Exit | General CLI | `verify` |
| --- | --- | --- |
| 0 | Command completed; inspect the selection outcome | Pass |
| 1 | Runtime error | Fail, apply failure or timeout |
| 2 | Usage error | Usage, task or config error |
| 3 | Config or task validation error | Verifier could not run |

Task formats remain backward compatible:

| `task.json` version | Contents |
| --- | --- |
| 1 | Coding: repository/revision, input files, `instruction.md`, and a Compose verifier |
| 2 | Adds reference diffs, mutants, doctor settings, and verifier kind/timeout |
| 3 | Explicit `face`: coding retains v2 fields; chat uses a conversation ending in a user message, with optional reference answer and rubric shown only to the judge |

`verify` supports exit-code and band-CSV verifiers; coding `select` supports
exit-code verifiers only. An empty reference diff verifies the revision unchanged.
CMoA verifies individual diffs; mutant generation and verifier-quality evaluation
belong to the layer above. Start with the [coding example](examples/task-hello/README.md)
or [chat example](examples/task-chat-hello/README.md).

## Selection and reproducibility

Chat selection first checks for a strict majority of agreeing, normalised
answers. Agreement selects a candidate without calling the judge. Otherwise,
every pair is compared in both orders: three candidates require six calls.
A win scores 1, a measured draw scores 0.5 per side, and a loss scores 0.
Ties are resolved by agreement, meaningful length differences, then text hashes;
byte-identical answers use the lowest candidate id. Malformed output, timeouts
and endpoint failures are tracked separately from measured draws. Malformed
judge output gets one retry.

Candidates are labelled A/B, sanitised and fenced; injection-shaped text is
flagged in the trace. The [selection decision](docs/adr/0013-consensus-then-copeland-for-chat-selection.md)
and [trace schema](docs/trace-schema.md) describe the detailed rules and outcomes.

- `propose --seed 7 --temperature 0` overrides every proposer's sampling settings.
- `judge --seed 7` controls the candidate-fence nonce; `--judge-seed` controls the
  judge's sampling seed. Neither shuffles the pairwise presentation.
- `judge --replay-from` reuses recorded candidates, seeds and answers in a new run,
  recording source digests. It requires a matching task and prompt version and
  cannot be combined with `--candidate`, `--seed` or `--judge-seed`.
- Completed selections are written once. Each run records effective settings,
  prompts, responses, candidates, timings, and the harness revision/date.

## Harnesses and traces

`propose`, `judge` and `serve` accept `--harness <dir>` for a rendered harness:
`system-prompt.md` extends CMoA's output contract, `memory/**/*.md` supplies notes
in path order, and `skills/<name>/SKILL.md` supplies names and descriptions.
Skill bodies are not executed or loaded into prompts. Harness input is validated,
its tree digest is recorded, and proposer context budgets include its content.
An empty directory is equivalent to no harness; `--harness ""` is an error.
See the [harness contract](docs/adr/0010-harness-directory.md).

`cmoa surfaces` lists editable harness components and their autonomy; the verifier,
tracer and model configuration remain read-only. CMoA declares these boundaries;
it does not edit itself.

Runs live under `<task>/runs/<run-id>/`. `run.json` records provenance,
`prompt/` and `candidates/` record generation, `verify/` or `judge/` record
assessment, and `select.json` records the outcome. Chat also writes `judge.json`.
See the [trace schema](docs/trace-schema.md) for file layouts, statuses and band CSV.

## Serve and monitor

```sh
./bin/cmoa serve --config "$CMOA_CONFIG"
# From another terminal:
curl http://127.0.0.1:8095/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"cmoa","messages":[{"role":"user","content":"What is 2 + 2?"}]}'
```

The chat-only server exposes `GET /v1/models` and `POST /v1/chat/completions`.
Successful responses include a `cmoa` field with selection metadata and a run id;
requests that create a run leave traces under `serve.runs_dir`. `stream: true`
returns one SSE chunk after selection, followed by `[DONE]`. No candidate or a
failed judge returns 502; a judge timeout returns 504. It binds loopback by
default, has no authentication or TLS, and requires `--allow-remote` to bind
elsewhere.

For each `POST /v1/chat/completions`, `serve` writes one terminal structured
record to its existing stderr logger. The line begins `performance ` and its
remainder is JSON; it is not a trace file or a standalone JSONL stream. The
server generates `request_id` and returns the same value in
`X-CMoA-Request-ID`; it does not use an inbound request-ID header. A record has
UTC `accepted_at` and `completed_at`, server-wall `parse_ms`, `queue_ms`,
`task_ms`, `propose_ms`, `select_ms`, `respond_ms`, `write_ms`, and `total_ms`,
plus an outcome. `started_at` is present after semaphore acquisition.
`queue_ms` is the wait from completed parsing to semaphore acquisition or queue
cancellation. `run_id` is included only once a run exists, and is distinct from
`request_id`, so join records to trace data through that field and keep queue
cancellation as an observation without a trace. `write_ms` ends when the
handler finishes writing the response; it does not show that a client received
it. The records exclude request content and headers, and describe this server
process rather than model-server or network time.

[CMoA Monitor](monitor/README.md) is a separate SvelteKit UI for fleet health,
run history, candidate/judge inspection and a chat panel. It reads traces and
server metrics; its chat panel forwards requests to `cmoa serve`.

To start both as containers from the repository root, with proposers and the
judge already listening at the URLs in `cmoa.json`:

```sh
make up                          # uses ./cmoa.json when it exists
CMOA_CONFIG=/path/to/cmoa.json make up
```

`make up` is `./deploy/up.sh`. Compose builds two images and runs them on the host
network so loopback fleet URLs keep working. The monitor binds `0.0.0.0:3999`
(`http://127.0.0.1:3999`). Ctrl+C stops both. It does not start model servers and
does not pass `--allow-remote`. Vault and `serve.runs_dir` must sit under
`CMOA_BIND`. Bind the monitor only to loopback with `CMOA_MONITOR_HOST=127.0.0.1`.
See [ADR 0017](docs/adr/0017-monitor-host-network-all-interfaces.md).

## Development

Code is organised by responsibility: CLI commands in `cmd/cmoa/`; configuration
and task loading in `internal/config/` and `internal/task/`; generation in
`internal/propose/`; selection in `internal/selection/` and `internal/judge/`;
HTTP in `internal/serve/`; record types and persistence in `internal/trace/`.
Coding verification uses `internal/patch/`, `internal/worktree/` and
`internal/verify/`. Prompt and harness rendering have their own packages.

```sh
make build test vet
make lint                       # requires golangci-lint
make docdag                     # requires docdag

# Live E2E: fresh run with a v2 config, proposer fleet, judge, DocDag and Docker.
CMOA_E2E=1 CMOA_CONFIG=/path/to/cmoa.json \
  go test -count=1 -timeout=30m -run '^TestE2E' -v ./...
```

Unit tests use local fixtures and fakes; no live models, Docker or DocDag are
required. Live E2E covers coding propose/select, reference and mutant verification,
and chat propose/select/judge. Monitor checks are documented in its own README.

Architecture decisions live in [docs/adr/](docs/adr/README.md). Query the active
set with `docdag query --binding`. Change a decision by adding a new record with
`docdag new "Title" --supersedes <id>`, then validate; accepted records preserve
history. `pre-commit install` enables the DocDag documentation checks.

## License

No license file has been added; the code and documents are not yet licensed for reuse.
