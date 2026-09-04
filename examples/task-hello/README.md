# task-hello

The smallest task CMoA can run end to end: a Go package whose `Add`
subtracts, and a test that says so.

```sh
./setup.sh                                  # copies src/ to repo/ and makes it a git repository
cmoa propose --task . --config /path/to/cmoa.json
cmoa select  --task .                       # verifies each candidate in docker
ls runs/*/                                  # the trace
```

`src/` is committed as ordinary files; `repo/` is generated and ignored, so
no git repository is nested inside this one.
