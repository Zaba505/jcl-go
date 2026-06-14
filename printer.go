// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"fmt"
	"io"
)

// Print the given [Job] to the given writer as JCL source.
func Print(w io.Writer, j *Job) error {
	pr := &printer{w: w}
	for action := printJob; action != nil && pr.err == nil; {
		action = action(pr, j)
	}
	return pr.err
}

type printer struct {
	w   io.Writer
	err error
}

// write emits s, short-circuiting once a previous write has failed.
func (pr *printer) write(s string) {
	if pr.err != nil {
		return
	}
	_, pr.err = io.WriteString(pr.w, s)
}

// writef emits a formatted string, short-circuiting once a previous write has
// failed.
func (pr *printer) writef(format string, args ...any) {
	if pr.err != nil {
		return
	}
	_, pr.err = fmt.Fprintf(pr.w, format, args...)
}

// printerAction is one step of the printer state machine: it writes some output
// and returns the next action. Returning nil ends printing. Errors are
// accumulated in pr.err rather than returned, so the driver loop stops on the
// first write failure.
type printerAction func(pr *printer, j *Job) printerAction

// writeThen writes a string and returns the next action — the printer
// equivalent of [yieldTokenThen].
func writeThen(s string, next printerAction) printerAction {
	return func(pr *printer, j *Job) printerAction {
		pr.write(s)
		return next
	}
}

// printJob is the entry action. It is a scaffold: the empty (zero-value) *Job
// prints nothing, so it ends immediately. Per-node printing is wired up in a
// later story.
func printJob(pr *printer, j *Job) printerAction {
	return nil
}
