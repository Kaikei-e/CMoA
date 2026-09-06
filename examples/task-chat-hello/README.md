# task-chat-hello

A one-item chat task, small enough to read in full and to run against a
local fleet in a minute. It exists so `cmoa propose`, `cmoa select` and
`cmoa judge` can be tried on the chat face without building a suite first.

```
task.json           version 3, face chat
conversation.json   three turns, ending with the user
reference.md        shown to the judge only
rubric.md           shown to the judge only
```

The proposers see the harness system prompt and then the three turns.
They never see `reference.md` or `rubric.md`: a proposer given the
reference answer is not answering the question.

```sh
cmoa propose --task . --config /path/to/cmoa.json
cmoa select  --task .            # asks the judge, writes judge.json and select.json
```

To judge answers produced somewhere else — which is what a calibration
does — skip the proposers:

```sh
cmoa judge --task . --candidate a.txt --candidate b.txt --candidate c.txt
```

`cmoa.json` must be version 2 and declare a `judge`. There is no gold
label here: this task shows the shape, and measuring the judge is a
calibration suite's job.
