// Package trace defines the on-disk record of one CMoA run and the writer
// that produces it. CMoA writes traces and never reads them back; the one
// exception is select, which reads the candidates propose left in the same
// run directory. uzushio and people read everything else.
//
// A run is one directory, <task>/runs/<run-id>/, laid out as:
//
//	run.json                        written once by propose
//	prompt/<proposer-id>.json       the exact request sent to each proposer
//	candidates/<proposer-id>.json   what came back, with a status
//	candidates/<proposer-id>.raw.txt
//	candidates/<proposer-id>.diff   the extracted diff, coding face only
//	candidates/<proposer-id>.txt    the answer, chat face only
//	verify/<proposer-id>/result.json  written by select, coding face only
//	verify/<proposer-id>/stdout.txt
//	verify/<proposer-id>/stderr.txt
//	judge/<pair>-<ab|ba>.json       one judge call, chat face only
//	judge.json                      written once by select or judge
//	select.json                     written once by select
//
// Every JSON file is written atomically (temp file, then rename). run.json
// and select.json refuse to overwrite an existing file: a run is appended
// to, never rewritten.
package trace
