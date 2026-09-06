# CMoA Monitor

A HUD for one CMoA round and the model fleet behind it.

The monitor reads run directories as `propose`, `select` and `judge` write them,
and polls each llama-server's `/slots` (falling back to `/metrics`). It pushes
the result to the browser over Server-Sent Events. It never runs a round and
never writes into a trace: the one thing it can start is a chat, and it starts
it by relaying the conversation to `cmoa serve`, which runs the round and writes
the trace the screen then reads back.

## Environment

| variable | meaning | default |
| --- | --- | --- |
| `CMOA_CONFIG` | path to a `cmoa.json`. `proposers[]`, `judge`, `serve` and `harness.vault` are read from it | **required** |
| `CMOA_MONITOR_ROOTS` | `:`-separated roots to watch. Each is a run directory (`run.json`), a task or request directory (`runs/`), or a serve root whose children hold `runs/`. The kind is detected, not configured | `serve.runs_dir` from the config, resolved against the config file |
| `CMOA_MONITOR_CALIBRATIONS` | directory of `kind: calibration` Markdown documents | `<harness.vault>/spec/calibrations` |
| `CMOA_MONITOR_INTERVAL_MS` | polling period | `500` |
| `HOST` / `PORT` | where the built server listens | `0.0.0.0` / `3000` |

Without `CMOA_CONFIG` the server starts and every API route answers `503` with
the reason, so the page can say what is missing.

## Running

```sh
pnpm install

# development
CMOA_CONFIG=/srv/cmoa/cmoa.json \
CMOA_MONITOR_ROOTS=/srv/cmoa/serve-runs:/srv/cmoa/tasks/task-hello \
pnpm dev --port 3999

# production (adapter-node)
pnpm build
CMOA_CONFIG=/srv/cmoa/cmoa.json \
CMOA_MONITOR_ROOTS=/srv/cmoa/serve-runs \
HOST=127.0.0.1 PORT=3999 node build
```

## HTTP surface

Every response is `no-store`.

| route | answers |
| --- | --- |
| `GET /api/stream?run=<id>` | SSE. Without `run` the stream follows the newest run and switches when a newer one appears |
| `GET /api/runs` | `{ runs: RunSummary[] }`, newest first |
| `GET /api/runs/<id>` | that run's `RunSnapshot` |
| `GET /api/runs/<id>/file?path=…` | one whitelisted trace file, capped at 256 KiB |
| `GET /api/calibration` | the newest calibration on file per judge |
| `POST /api/chat` | relays `{ messages }` to `cmoa serve` and answers with what it said |

`POST /api/chat` takes `{ messages: [{ role, content }] }` — every role but the
last may be `system`, `assistant` or `user`, the last must be `user`, no content
may be empty and the body is capped at 1 MiB. It forwards `{ model:
<serve.pool_name>, messages, stream: false }` to
`http://<serve.listen>/v1/chat/completions`, waits up to ten minutes (a round is
minutes, not seconds) and returns the upstream body and status unchanged: a
`200` with `choices[0].message.content` and the `cmoa` extension, or CMoA's own
`{ error: { message, type, param, code } }` with `502` for `no_candidate` and
`judge_failed` and `504` for `judge_timeout`. A pool already busy with
`max_inflight` rounds queues the request rather than refusing it. Without a
`serve` block in the config the route answers `503`, and an unreachable pool is
`502 cmoa serve unreachable at <listen>`.

SSE events:

- `fleet` — every tick: reachability, slots, decoded tokens and a smoothed
  tokens/second per server, plus `cmoa serve` health.
- `run` — whenever the derived snapshot changes: header, proposer lanes, the
  judge grid (chat face), select and the phase timeline.
- `runs` — whenever the run list changes.
- `calibration` — on connect and whenever the documents change.
- `: keep-alive` comments every 15 s on a quiet connection.

## Screen

One page, one URL. `?run=<id>` pins a run; without it the screen follows
whichever run is newest and switches when a newer one appears.

| panel | what it holds |
| --- | --- |
| `FLEET` | one cell per proposer plus `JUDGE` and `SERVE`: reach lamp, model, state word, decoded tokens, tokens/second, prefill progress. Always live, run or no run |
| `ROUND` | run id, face, elapsed, phase, the run directory, and the `FOLLOW` / `PINNED` chip |
| `PROPOSE` | one row per proposer: state, a segmented budget bar, decoded tokens, rate; underneath, the candidate status, the answer excerpt or `N file(s) +a/-d`, timing, and `verify <status>` (coding) or reasoning bytes (chat) |
| `JUDGE` | chat only: the judge server, the presentation seed and nonce, the pair x ab/ba grid with its verdict column, wins, swap-consistency, retries and outcome. A verdict inferred from the call files before `judge.json` lands is tagged `PROV` |
| `SELECT` | the selection sentence, the ranking, and anything else that passed |
| `TIMELINE` | a time axis with a marker per phase, and the same offsets as text |
| `CALIBRATION` | per judge: verdict, human / swap / rerun kappa, tie handling, and whether it is still in force |
| `ROOTS` | where the data came from |
| `COMM` | a chat with the pool through `cmoa serve`: the transcript, the run behind each answer, and the compose box |

Clicking a proposer row opens its candidate in `INSPECT`; clicking a judge cell
opens that call file.

### COMM

`COMM` sends the whole transcript — every prior user and assistant turn, in
order — to `POST /api/chat`, because `cmoa serve` keeps no session of its own.
Enter sends, Shift+Enter is a newline, and `SEND` is dead while a round is in
flight or while the pool is unreachable, which the panel says as `SERVE
OFFLINE`. Sending switches the HUD to follow mode so the new round appears as
`cmoa serve` writes it, and the answer's run is pinned when it lands, so the
round that produced it stays on screen. Each answer carries a dim line —
`run <id> · selected · 2/3 swap-consistent · 23.2s` — whose run id pins that
run.

A round that selected nobody is not an answer with an apology: the panel shows
`NO CANDIDATE (all_draws) run <id>` in red, with no assistant line, and the run
id opens the round that refused. The question stays in the transcript, struck
through, so it can be edited and sent again; it is not forwarded as history in
later turns, because a round that never happened is not part of the
conversation. The transcript lives in `sessionStorage` and lasts as long as the
tab. `CLEAR` empties it.

| key | does |
| --- | --- |
| `R` | the `RUNS` drawer (left): the run history, newest first. A row pins that run; `FOLLOW NEWEST` returns to following |
| `I` | the `INSPECT` drawer (right): any whitelisted trace file of the run on screen, pretty-printed when it is JSON and badged `TRUNCATED` when it was cut |
| `F` | back to follow mode |
| `C` | focus the `COMM` compose box. Typing there never triggers these keys |
| `Esc` | close the open drawer |

`MOTION ON/OFF` in the status bar stops the state fades and the link-lost
blink; `prefers-reduced-motion` does the same without being asked. The status
bar also carries the key to the four state colours, one word each. Losing the
stream shows `LINK LOST` and reconnects with exponential backoff. A missing or
unreadable `CMOA_CONFIG` shows as a `CONFIG FAULT` panel rather than an empty
screen.

## Tests

```sh
pnpm test:unit   # vitest, no network and no fleet needed
pnpm check       # svelte-check: must be 0 errors and 0 warnings
pnpm build
pnpm test:e2e    # playwright, chromium only
```

The end-to-end suite builds the app and runs `node build` itself. Before the
tests, `tests/e2e/global-setup.ts` starts a fake fleet -- plain
`http.createServer` instances answering `/slots`, `/metrics`, `/v1/models` and
a canned `POST /v1/chat/completions` on ports 8481-8483, 8490 and 8495 -- plus a control endpoint on 8499 that a
test uses to take one server away and give it back. `tests/fixtures/e2e/cmoa.json`
points the monitor at those ports, and the environment points it at the fixture
serve root, the fixture coding task and the fixture calibrations. Install the
browser once with `pnpm exec playwright install --with-deps chromium`.

The fixtures under `tests/fixtures/` carry the real trace structure with
synthetic bodies: no candidate answer, diff or judge reason in them came from a
model, and every absolute path in them is fictional.
