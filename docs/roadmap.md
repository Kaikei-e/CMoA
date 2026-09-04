# Roadmap

CMoA is one layer of a three-layer stack, and its milestones are ordered
by what the layers above can verify. Each step lands only after the
previous one runs unattended for a while; the order is fixed, the dates
are not.

| step | layer | what lands | status |
| --- | --- | --- | --- |
| 1 | DocDag | public `config` package and YAML round-trip tests, so uzushio can build its vault configuration in Go | pending |
| 2 | uzushio | vault configuration written in Go; `docdag lint` and `lint --fixtures` pass | pending |
| 3 | uzushio | `task doctor`: kill rate against injected defects, false-positive rate against a reference solution | pending |
| 4 | **CMoA** | **v0, coding face: `propose` and `select`, verifier-selected, no judge** | shipped (this repository) |
| 5 | uzushio | `run` and `improve`: held-in and held-out splits, sequential testing, edits accepted only when both pass | pending |
| 6 | uzushio | the first task manifest carries the constraints learned from the previous project: one milestone per session, a ceiling on test lines per product line, dogfooding kept off the critical path | pending |
| 7 | CMoA | chat face: a single blind judge on a separate accelerator, randomised and position-swapped presentation, calibration log | after 1–6 |

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

## Decisions behind the order

The architecture decision records under [`adr/`](adr/) record why each
piece is shaped as it is. The order above follows from one of them: a
feature without a verifier is not started, and the verifier's own
soundness is measured before it is trusted.
