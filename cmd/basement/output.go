// Package main — output.go centralises table + JSON rendering so each
// subcommand only describes its columns once. Two render modes:
//
//	--output-format table (default)  human-aligned columns to stdout
//	--output-format json             pretty-indented JSON to stdout
//
// Both modes share the same shape — render(out io.Writer, rows) — so
// tests can capture into a bytes.Buffer instead of stdout.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// renderTable writes a header row + data rows aligned in columns. The
// rows slice is column-major-by-row — each entry is one row's column
// values. Caller is responsible for stringifying values.
func renderTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// renderJSON writes any value as indented JSON. Used when
// --output-format=json. The render functions in each subcommand pass
// the unflattened model so machine consumers see the rich shape, not
// the table-row strings.
func renderJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// outputTable returns true if the active --output-format wants a
// table. Centralised so each subcommand renders consistently.
func outputTable() bool {
	return strings.EqualFold(ctx.outputFormatFlag, "table") || ctx.outputFormatFlag == ""
}

// outputJSON renders v as JSON to the supplied writer when
// --output-format=json. Subcommands typically call:
//
//	if !outputTable() { return outputJSON(cmd.OutOrStdout(), model) }
//	renderTable(cmd.OutOrStdout(), ...)
//
// Routing through cmd.OutOrStdout() means tests can capture output via
// a bytes.Buffer instead of stealing os.Stdout.
func outputJSON(w io.Writer, v any) error {
	return renderJSON(w, v)
}
