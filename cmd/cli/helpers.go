/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"
)

// formatAge returns a human-readable duration string similar to kubectl output.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// tabWriterHelper wraps a *tabwriter.Writer and accumulates the first write
// error so callers can check errors at the end via Flush() instead of after
// each individual write. Only the first write error is preserved; subsequent
// writes are silently skipped once an error has occurred. This pattern is
// safe because tabwriter buffers all output and errors are propagated through
// Flush() anyway.
type tabWriterHelper struct {
	tw  *tabwriter.Writer
	err error
}

func newTabWriterHelper(w io.Writer) *tabWriterHelper {
	return &tabWriterHelper{tw: tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)}
}

func newStdoutTabWriterHelper() *tabWriterHelper {
	return newTabWriterHelper(os.Stdout)
}

// Printf formats and writes to the underlying tabwriter.
// If a previous write already failed the call is a no-op.
func (t *tabWriterHelper) Printf(format string, args ...any) {
	if t.err != nil {
		return
	}
	_, t.err = fmt.Fprintf(t.tw, format, args...)
}

// Println writes s followed by a newline to the underlying tabwriter.
// If a previous write already failed the call is a no-op.
func (t *tabWriterHelper) Println(s string) {
	if t.err != nil {
		return
	}
	_, t.err = fmt.Fprintln(t.tw, s)
}

func (t *tabWriterHelper) Flush() error {
	if t.err != nil {
		return t.err
	}
	return t.tw.Flush()
}
