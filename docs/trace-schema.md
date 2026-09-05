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
  candidates/<proposer-id>.diff   coding face, only when status is ok
  candidates/<proposer-id>.txt    chat face, only when status is ok
  verify/<proposer-id>/result.json  coding face, written by select
  verify/<proposer-id>/stdout.txt
  verify/<proposer-id>/stderr.txt
  judge/<pair>-<ab|ba>.json       chat face, one judge call each
  judge.json                      chat face, written once by select or judge
  select.json                     written once by select
```

Which files a run holds follows from `run.json`'s `face`. The coding face
writes diffs and `verify/`; the chat face writes answers, `judge/` and
`judge.json`. Nothing else differs: the run id, the atomic writes and the
write-once rule are the same on both.

`run-id` is `YYYYMMDDTHHMMSSZ-<8 hex>` in UTC, so the lexicographically
largest directory is the latest run. Every JSON file is written atomically.
`run.json` and `select.json` are write-once.

## run.json

| field | meaning |
| --- | --- |
| `schema_version` | 1 |
| `run_id`, `created_at`, `cmoa_version`, `prompt_version` | identity of the run and of the code and prompt templates that produced it |
| `face` | `coding` or `chat`; a run written before the field existed has none, and is a coding run |
| `task` | `id`, `dir`, `repo`, `rev` (as written), `resolved_rev` (commit SHA), `files`, `instruction_sha256`. On the chat face `repo`, `rev`, `resolved_rev` and `files` are empty: nothing is checked out |
| `conversation_sha256` | chat face: the digest of the conversation as CMoA parsed it |
| `candidates_origin` | chat face: `proposers`, or `external` when `cmoa judge` was handed the answers |
| `external_candidates` | chat face, `external` only: `id`, `file` and `sha256` per answer, so a caller can pin what was judged |
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
| `status` | `ok` (a diff was extracted, or an answer arrived), `http_error`, `timeout`, `malformed` (2xx but not a chat completion), `no_diff` (coding face: a completion with no unified diff in it), `empty` (chat face: a completion with no text in it) |
| `face`, `origin` | the run's face; `origin` is `external` for an answer `cmoa judge` was handed |
| `error` | the error text for anything but `ok` |
| `finish_reason`, `usage.prompt_tokens`, `usage.completion_tokens` | as the server reported |
| `timings.request_ms` | measured by CMoA; `server_prompt_ms`, `server_predicted_ms`, `tokens_per_second` come from llama-server's `timings` and are absent on other servers |
| `diff` | coding face: `files`, `additions`, `deletions`, `sha256` of the `.diff` file; only when `status` is `ok` |
| `answer_sha256`, `answer_bytes` | chat face: of the `.txt` file; only when `status` is `ok` |
| `metadata` | chat face: `token_len` (the server's `completion_tokens`, or `-1`), `chars`, `header_count`, `list_count`, `bold_count`, `code_fence_count` |
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
| `rule` | `first` on the coding face, `judge-pairwise` on the chat face |
| `order` | candidate ids in the order they were considered (configured order, or `c1..cN` for external candidates) |
| `selection.kind` | `selected` (`candidate_id`, `reason`), `no_candidate` (`tried`, and on the chat face `reason`, the sub-reason below), `verifier_failed` (`error`), `judge_timeout` (`after_ms`), `judge_failed` (`error`, chat face) |
| `also_passed` | coding face: other candidates that passed; every candidate is verified even after the first pass, so the layer above can measure how often proposers agree. Always `[]` on the chat face |
| `ranked` | chat face: candidate ids by wins, ties broken by `order`. Informational — only `selection` decides |
| `max_parallel`, `finished_at` | `max_parallel` is `verify.max_parallel` on the coding face and `judge.parallel` on the chat face |

## the chat face

A chat task (`task.json` version 3, `face: "chat"`) has no repository and
no verifier. Its `conversation.json` is a JSON array of `{role, content}`
with roles `system|user|assistant`, at least one message, non-empty content
on each, and a `user` message last — that last message is what the
proposers answer. `reference.answer` and `rubric` are Markdown files shown
to the **judge only**; a proposer handed the reference answer is not
answering the question. `judge.allow_tie` (default true) decides whether
`tie` is in the judge's answer schema. An `instruction.md` in a chat task
directory is ignored, with a log line.

`max_context_bytes` bounds the conversation plus the rendered harness, the
way it bounds the instruction and the files on the coding face.

### candidates/<id>.txt

The selected-from answer, reasoning blocks stripped and outer whitespace
trimmed. `.raw.txt` still holds the completion exactly as it arrived. A
candidate whose answer was only whitespace has status `empty` and writes no
`.txt`.

The `metadata` block is the style accounting a preference harness records
for every answer. None of it reaches the judge's prompt. It is written at generation
time because the question it answers — *was the judge buying length and
decoration?* — cannot be answered later from numbers nobody wrote down.

### the protocol

Three candidates make three pairs; each pair is asked in both orders, so a
selection is six calls. A pair is won only when **both orders name the same
candidate**; a disagreement, or a `tie` in either order, is a draw and
scores nothing for either side. A candidate that wins every pair it appears
in is the Condorcet winner and is selected. Anything else is
`no_candidate`, with a sub-reason:

| sub-reason | when |
| --- | --- |
| `cycle` | every pair was decided and the wins run in a circle |
| `no_majority` | some pair was decided, but nobody beat everybody |
| `all_draws` | no pair was decided at all (two or more pairs) |
| `invalid_output` | the judge never returned usable JSON for a call the outcome needed, retry included |
| `too_few_candidates` | fewer than two answers to compare |

There is no re-ask beyond one retry for malformed JSON, and no
deterministic fallback. "Take the first" or "take the shorter" would
reinstate as a rule exactly the position and length biases the order swap
exists to detect.

The presentation order is a permutation seeded from the run id (or from
`--seed`), recorded in `judge.json`. The candidates are called `A` and `B`
inside a call; which candidate is which is only in the trace.

### judge.json

```json
{
  "schema_version": 1,
  "run_id": "20260905T120000Z-abcdef01",
  "judge": {"model": "…", "base_url": "…", "temperature": 0, "seed": null, "max_tokens": 512,
            "output_format": "json_schema", "parallel": 3, "allow_tie": true, "prompt_version": "…",
            "extra_body": {"chat_template_kwargs": {"reasoning_effort": "low"}}},
  "candidates": ["p1", "p2", "p3"],
  "presentation": {"permutation": [2, 0, 1], "nonce": "7f3a91c4", "seed_source": "run_id"},
  "pairs": [
    {"pair": ["p1", "p2"],
     "orders": [
       {"first": "p1", "second": "p2", "choice": "A", "choice_candidate": "p1", "status": "ok",
        "retries": 0, "latency_ms": 3200, "request_sha256": "…", "response_sha256": "…",
        "file": "judge/0-ab.json"},
       {"first": "p2", "second": "p1", "choice": "B", "choice_candidate": "p1", "status": "ok",
        "retries": 1, "latency_ms": 3400, "request_sha256": "…", "response_sha256": "…",
        "file": "judge/0-ba.json"}
     ],
     "verdict": "p1"
    }
  ],
  "wins": {"p1": 2, "p2": 0, "p3": 1},
  "outcome": {"kind": "selected", "candidate_id": "p1", "reason": "condorcet winner, 2 of 3 pairs agreed under both orders"},
  "ranked": ["p1", "p3", "p2"],
  "swap_consistent_pairs": 3,
  "invalid_output_retries": 1,
  "sanitized": [{"candidate": "p2", "what": "closing-tag-like sequence escaped", "count": 1}],
  "injection_flags": {"p1": [], "p2": ["ignore previous instructions"], "p3": []},
  "usage": {"prompt_tokens": 4800, "completion_tokens": 210},
  "latency_ms": 41230,
  "finished_at": "…"
}
```

`verdict` is a candidate id or `draw`. An order's `status` is `ok`,
`invalid_output` (still unparsable after the one retry), `timeout` or
`error` (HTTP or decode failure). One `timeout` makes the whole outcome
`judge_timeout`; one `error` makes it `judge_failed`. Neither says anything
about any candidate: the question was never put.

`swap_consistent_pairs` counts pairs whose two orders named the **same
candidate** — including two ties. Choosing `A` in both orders is not
agreement; it is the position speaking.

`sanitized` is what the fencing rewrote before the judge saw it. A rewrite
changes what is judged, so a silent one would make an outcome
unexplainable. Two rewrites exist: a closing-tag-like sequence is escaped
(`</candidate` becomes `<\/candidate`), and the C0 control characters other
than tab and newline are dropped.

`injection_flags` lists the injection-shaped phrases a candidate's answer
held. They are **recorded and never acted on**: silently discarding a
flagged candidate would be a second, unmeasured judge, and the flag's value
is that a calibration can ask whether flagged candidates win more often
than they should.

### judge/<pair>-<ab|ba>.json

One call, in full: the pair and its order, the model and base URL, and one
`attempts` entry per HTTP round trip. Each attempt holds the exact messages
sent, the exact request body (no `Authorization`), the exact response body,
the completion text with reasoning stripped, and either the parsed object
or the parse error. A second attempt exists only when the first did not
parse; it appends exactly one message, `Return only the JSON object.`

The candidate blocks in the prompt are fenced with a per-selection nonce:

```
<candidate id="A" n="7f3a91c4">
…
</candidate:7f3a91c4>
```

### how the judge is asked

`judge.output_format` is `json_schema` (the default) or `none`. With
`json_schema` CMoA sends an OpenAI `response_format` naming an object with
`reason` (a string of at most 400 characters) then `choice` (the enum
`A`, `B`, and `tie` when the task allows one). The key order is the
schema's: the reason is written before the choice, so the choice cannot be
reached without passing through it.

CMoA never sends a raw GBNF `grammar`. A server with its own structured
chat format parses a raw grammar beside that format rather than composing
the two, and answers an error; `judge.extra_body` refuses both `grammar`
and `response_format` for that reason.

Parsing takes the **last** balanced `{…}` in the completion, ignoring
braces inside JSON strings, and decodes it with unknown fields refused. A
model that reasons in prose before answering leaves earlier braces behind,
and the answer is the one it finished with.

### cmoa judge

`cmoa judge --task <chat task> --candidate <file> …` builds a run from
answers produced somewhere else and performs the same protocol with no
proposer call. The candidates are `c1..cN` in the order the flags were
given; `run.json` records `candidates_origin: "external"` and each file's
digest. `--seed` changes the presentation permutation and the nonce, never
the judge's sampling seed — `--judge-seed` does that.

On the chat face both `cmoa select` and `cmoa judge` print one JSON object
on stdout:

```json
{"kind":"selected","candidate_id":"c1","reason":"condorcet winner, 2 of 3 pairs agreed under both orders",
 "answer":"…/candidates/c1.txt","ranked":["c1","c3","c2"],"run":"…","judge":"…/judge.json"}
```

`cmoa judge` prints the run directory on a second line. Both exit 0
whatever the outcome.

### cmoa serve

`cmoa serve` answers `GET /v1/models` and `POST /v1/chat/completions`.
Every request writes a task directory under `serve.runs_dir` holding a
generated `task.json` and `conversation.json`, and then a full run trace
inside it, so an answer served over HTTP is as reconstructible as one
produced by the CLI. The task id is the run id lower-cased, so the task,
the run and the completion id all name the same request.

A 200 carries the usual chat completion plus a `cmoa` extension field:

```json
"cmoa": {"run_id": "…", "selection": {"kind": "selected", "reason": "…"},
         "judge": {"calls": 6, "swap_consistent_pairs": 3, "invalid_output_retries": 0, "latency_ms": 41230},
         "candidates": {"asked": 3, "ok": 3}, "harness": {"tree_sha256": "…"}}
```

`usage` sums the proposers and the judge. The id of the proposer whose
answer won is **not** in the response — it is in the trace. A client that
could see it could learn to ask for it, and the pool is the router's
decision.

A selection that did not happen is an error, not a 200 with an apology:

| status | `error.type` | when |
| --- | --- | --- |
| 400 | `invalid_request_error` | the body, or the messages, did not validate |
| 404 | `invalid_request_error` | `model` is not `serve.pool_name` |
| 502 | `no_candidate` | `error.code` is the sub-reason, `error.param` the run id |
| 502 | `judge_failed` | the judge could not be asked |
| 504 | `judge_timeout` | the judge did not answer in time |

`stream: true` returns `text/event-stream` with one
`chat.completion.chunk` carrying the whole content and then `data: [DONE]`.
The answer is chosen before a single token can be sent — the judge cannot
compare answers that do not exist yet — so this is the wire format, not
streaming.

`serve.max_inflight` (default 1) bounds selections in flight: a second
judge call halves the accelerator the first is using, and every latency in
the trace would become a measurement of contention. The server binds
loopback, has no auth and no TLS; a non-loopback `serve.listen` needs
`--allow-remote` on the command line, where a person types it.
