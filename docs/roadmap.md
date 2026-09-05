# Roadmap

CMoA is one layer of a three-layer stack, and its milestones are ordered
by what the layers above can verify. Each step lands only after the
previous one runs unattended for a while; the order is fixed, the dates
are not.

| step | layer | what lands | status |
| --- | --- | --- | --- |
| 1 | DocDag | public `config` package and YAML round-trip tests, so uzushio can build its vault configuration in Go | shipped ([DocDag v0.4.0](https://github.com/Kaikei-e/DocDag/releases/tag/v0.4.0), 2026-09-04) |
| 2 | uzushio | vault configuration written in Go; `docdag lint` and `lint --fixtures` pass | shipped ([uzushio#1](https://github.com/Kaikei-e/uzushio/pull/1), 2026-09-05) |
| 3 | uzushio | `task doctor`: kill rate against injected defects, false-positive rate against a reference solution; `task mutate` for Go mutants; `task calibrate` for banded verifiers. CMoA contributes `cmoa verify` with the `exit-code` and `band` kinds and `task.json` version 2 ([ADR 0009](adr/0009-add-verify-command-and-task-v2.md)) | shipped ([uzushio#2](https://github.com/Kaikei-e/uzushio/pull/2), [#4](https://github.com/Kaikei-e/uzushio/pull/4), [#5](https://github.com/Kaikei-e/uzushio/pull/5), 2026-09-05) |
| 4 | **CMoA** | **v0, coding face: `propose` and `select`, verifier-selected, no judge** | shipped (this repository) |
| 5 | uzushio | `run` and `improve`: held-in and held-out splits, sequential testing, edits accepted only when both pass | pending |
| 6 | uzushio | the first task manifest carries the constraints learned from the previous project: one milestone per session, a ceiling on test lines per product line, dogfooding kept off the critical path | pending |
| 7 | CMoA | chat face: a single blind judge on a separate accelerator, randomised and position-swapped presentation, calibration log | after 1–6 |

## What v1 does not do

- **The judge never writes an answer.** It compares candidates and picks
  one, or picks none. Nothing is merged, rewritten or completed, so an
  answer that reaches a caller is an answer a proposer wrote.
- **No panel of judges.** One judge, measured by calibration. Nine
  frontier judges are worth about two effective votes on correlated
  errors, and the panel's accuracy sits well below what independent votes
  would give — so a panel buys far less than it costs, and hides the one
  thing that can be measured.
- **No deterministic fallback.** When the pairwise protocol does not
  produce a Condorcet winner the outcome is `no_candidate` with a
  sub-reason, not "the first" or "the shorter". A fallback rule would
  reinstate as a design decision exactly the position and length biases
  the order swap exists to detect.
- **No re-asking.** One retry for JSON that did not parse, and nothing
  else. Repeated asking of a judge that is unsure makes a coin flip look
  like a decision.
- No pool selection by measured error correlation. The pool is the
  configured list, in configured order; the correlation matrix is a
  uzushio measurement that a later CMoA version will read.
- No self-editing. CMoA declares which harness surfaces may be edited
  (`cmoa surfaces`); proposing and validating edits is uzushio's loop.
- `cmoa serve` is the chat face only, and it is not a daemon. It has no
  auth, no TLS and no scheduler; it binds loopback, runs one selection at
  a time by default, and writes the same trace the CLI writes. The coding
  face has no server: a verifier needs a repository and a container, which
  is not something to hand an HTTP client.
- No mutant generation and no kill-rate arithmetic. `cmoa verify` answers
  one question about one diff — by exit code, or by reading a banded gate's
  own CSV verdicts; reading `reference` and `mutants` out of `task.json` is
  as far as CMoA goes, and `task doctor`, `task mutate` and `task calibrate`
  are uzushio's. CMoA holds no copy of any gate's thresholds.

## Decisions behind the order

The architecture decision records under [`adr/`](adr/) record why each
piece is shaped as it is. The order above follows from one of them: a
feature without a verifier is not started, and the verifier's own
soundness is measured before it is trusted.
