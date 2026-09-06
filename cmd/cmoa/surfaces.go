package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/Kaikei-e/CMoA"
)

func cmdSurfaces(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("surfaces", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if code, done := parseArgs(fs, args); done {
		return code
	}
	type row struct {
		Surface  cmoa.Surface  `json:"surface"`
		Autonomy cmoa.Autonomy `json:"autonomy"`
	}
	var rows []row
	for _, s := range cmoa.AllSurfaces() {
		rows = append(rows, row{s, s.Autonomy()})
	}
	switch *format {
	case "json":
		out := struct {
			Surfaces []row                    `json:"surfaces"`
			ReadOnly []cmoa.ReadOnlyComponent `json:"read_only"`
		}{rows, cmoa.ReadOnlyComponents()}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return exitRuntime
		}
	case "text":
		for _, r := range rows {
			fmt.Fprintf(stdout, "%-20s %s\n", r.Surface, r.Autonomy)
		}
		for _, c := range cmoa.ReadOnlyComponents() {
			fmt.Fprintf(stdout, "%-20s read-only\n", c)
		}
	default:
		fmt.Fprintf(stderr, "cmoa: --format must be text or json\n")
		return exitUsage
	}
	return exitOK
}
