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
| 5 | uzushio | `run` and `improve`: held-in and held-out splits, sequential testing, edits accepted only when both pass. CMoA contributes `propose --harness`, `--seed` and `--temperature` ([ADR 0010](adr/0010-harness-directory.md)) | shipped ([uzushio#6](https://github.com/Kaikei-e/uzushio/pull/6), 2026-09-05) |
| 6 | uzushio | the first task manifest carries the constraints learned from the previous project: one milestone per session, a ceiling on test lines per product line, dogfooding kept off the critical path | shipped ([uzushio#6](https://github.com/Kaikei-e/uzushio/pull/6), clauses UZ-C-006 to UZ-C-008, 2026-09-05) |
| 7 | CMoA | chat face: a single blind judge on a separate accelerator, randomised and position-swapped presentation, calibration log | in progress |

## What v0 does not do

- No judge model. The coding face selects by verifier pass alone.
- No daemon or HTTP server. `cmoa` is a CLI that the layer above calls.
- No retries or repair rounds. A malformed answer is a candidate with a
  status, recorded for the layer above to mine.
- No pool selection by measured error correlation. The pool is the
  configured list, in configured order; the correlation matrix is a
  uzushio measurement that a later CMoA version will read.
- No self-editing. CMoA declares which harness surfaces may be edited
  (`cmoa surfaces`); proposing and validating edits is uzushio's loop.
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
