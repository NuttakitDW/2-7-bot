package main

import (
	"encoding/json"
	"os"
	"text/tabwriter"
)

// printJSON writes an indented JSON document to stdout.
func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// newTable returns a tab-aligned writer for column output. Callers must Flush.
func newTable() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

// shortDigest trims a SHA-256 to the 12 characters the platform's UI shows.
func shortDigest(digest string) string {
	if len(digest) <= 12 {
		return digest
	}
	return digest[:12]
}
