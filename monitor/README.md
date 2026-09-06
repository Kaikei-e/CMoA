# CMoA Monitor

A read-only HUD for one CMoA round and the model fleet behind it.

The monitor reads run directories as `propose`, `select` and `judge` write them,
and polls each llama-server's `/slots` (falling back to `/metrics`). It pushes
the result to the browser over Server-Sent Events. It never starts a round,
never writes into a trace, and asks `cmoa serve` for nothing but `GET
/v1/models` to see whether it is up.

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
| `ROOTS`, `LEGEND` | where the data came from, and the key to the four state colours |

Clicking a proposer row opens its candidate in `INSPECT`; clicking a judge cell
opens that call file.

| key | does |
| --- | --- |
| `R` | the `RUNS` drawer (left): the run history, newest first. A row pins that run; `FOLLOW NEWEST` returns to following |
| `I` | the `INSPECT` drawer (right): any whitelisted trace file of the run on screen, pretty-printed when it is JSON and badged `TRUNCATED` when it was cut |
| `F` | back to follow mode |
| `Esc` | close the open drawer |

`MOTION ON/OFF` in the status bar stops the radar sweep and the link-lost
blink; `prefers-reduced-motion` does the same without being asked. Losing the
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
`http.createServer` instances answering `/slots`, `/metrics` and `/v1/models`
on ports 8481-8483, 8490 and 8495 -- plus a control endpoint on 8499 that a
test uses to take one server away and give it back. `tests/fixtures/e2e/cmoa.json`
points the monitor at those ports, and the environment points it at the fixture
serve root, the fixture coding task and the fixture calibrations. Install the
browser once with `pnpm exec playwright install --with-deps chromium`.

The fixtures under `tests/fixtures/` carry the real trace structure with
synthetic bodies: no candidate answer, diff or judge reason in them came from a
model, and every absolute path in them is fictional.
