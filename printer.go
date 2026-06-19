// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"fmt"
	"io"
	"strings"
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

const (
	// operationColumn is the conventional column where the operation field
	// begins (the name field is padded to reach it).
	operationColumn = 12
	// operandColumn is the conventional column where the parameter (operand)
	// field begins (the operation field is padded to reach it).
	operandColumn = 17
)

// printJob is the entry action. It walks the preamble, then the JOB statement,
// then the body. The empty (zero-value) *Job has no preamble and no JOB
// statement, so it prints nothing.
func printJob(pr *printer, j *Job) printerAction {
	return printPreambleAt(0)
}

// printPreambleAt returns an action that prints the preamble statement at index
// i, then advances to i+1; once the preamble is exhausted it hands off to the JOB
// statement. Same closure-over-index pattern as printBodyAt.
func printPreambleAt(i int) printerAction {
	return func(pr *printer, j *Job) printerAction {
		if i >= len(j.Preamble) {
			return printJobStatement
		}
		writeTrivial(pr, j.Preamble[i])
		return printPreambleAt(i + 1)
	}
}

// printJobStatement writes the JOB statement, then hands off to the body walker.
// A nil Statement — the empty-input parse or an input of only preamble statements
// — ends printing.
func printJobStatement(pr *printer, j *Job) printerAction {
	if j.Statement == nil {
		return nil
	}
	writeStatement(pr, j.Statement.Name, "JOB", j.Statement.Parameters)
	return printBodyAt(0)
}

// printBodyAt returns an action that prints the body statement at index i, then
// advances to i+1; it returns nil once the body is exhausted. This is the
// closure-over-index slice pattern, mirroring the tokenizer's closures rather
// than holding iterator state on the printer struct.
func printBodyAt(i int) printerAction {
	return func(pr *printer, j *Job) printerAction {
		if i >= len(j.Body) {
			return nil
		}
		switch s := j.Body[i].(type) {
		case *ExecStatement:
			writeStatement(pr, s.Name, "EXEC", s.Parameters)
			// A step's DDs are nested on the ExecStatement, but trivial records
			// (comment/null/delimiter) coded among them live in Job.Body. Gather the
			// trivial records up to the next step and print them interleaved with the
			// DDs in source order, so a comment coded between an EXEC and its DD is
			// not hoisted past the DD.
			k := i + 1
			for k < len(j.Body) {
				if _, isExec := j.Body[k].(*ExecStatement); isExec {
					break
				}
				k++
			}
			writeStepRecords(pr, s, j.Body[i+1:k])
			return printBodyAt(k)
		case *CommentStatement, *NullStatement, *DelimiterStatement:
			writeTrivial(pr, s)
			return printBodyAt(i + 1)
		}
		return printBodyAt(i + 1)
	}
}

// writeTrivial writes a non-semantic statement record — a comment, null, or
// delimiter — followed by a newline. A comment statement's text is the full
// record (it includes the leading "//*"), so it is written verbatim.
func writeTrivial(pr *printer, s Statement) {
	switch s := s.(type) {
	case *CommentStatement:
		pr.write(s.Text)
	case *NullStatement:
		pr.write("//")
	case *DelimiterStatement:
		pr.write("/*")
	}
	pr.write("\n")
}

// writeStepRecords writes a step's DD statements interleaved with the trivial
// records (comment/null/delimiter) that follow the EXEC in the body, restoring
// source order. The DDs are nested on the ExecStatement while the trivial records
// live in Job.Body, so without this merge a comment coded between the EXEC and a DD
// would be hoisted past the DD. Records are merged by source position; a DD sorts
// before a trivial record at the same position, so a zero-position (hand-built) AST
// still prints its DDs immediately after the EXEC.
func writeStepRecords(pr *printer, s *ExecStatement, trivials []Statement) {
	dds := flattenDDs(s)
	di, ti := 0, 0
	for di < len(dds) && ti < len(trivials) {
		if posLess(statementPos(trivials[ti]), dds[di].dd.Pos) {
			writeTrivial(pr, trivials[ti])
			ti++
			continue
		}
		writeDD(pr, dds[di])
		di++
	}
	for ; di < len(dds); di++ {
		writeDD(pr, dds[di])
	}
	for ; ti < len(trivials); ti++ {
		writeTrivial(pr, trivials[ti])
	}
}

// ddRecord is one physical DD record paired with the name it prints under: the
// concatenation's ddname for a group's first member, nil for its continuations.
type ddRecord struct {
	name *Name
	dd   *DDStatement
}

// flattenDDs flattens a step's concatenation groups into the physical DD records
// in source order: each group's first member carries the ddname, the rest a blank
// name field.
func flattenDDs(s *ExecStatement) []ddRecord {
	var out []ddRecord
	for _, c := range s.DDs {
		for i, dd := range c.DDs {
			var name *Name
			if i == 0 {
				name = c.Name
			}
			out = append(out, ddRecord{name: name, dd: dd})
		}
	}
	return out
}

// writeDD writes a single DD record under its (possibly blank) name field.
func writeDD(pr *printer, r ddRecord) {
	writeStatement(pr, r.name, "DD", r.dd.Parameters)
}

// statementPos returns the source position of a body statement, used to merge
// nested DDs with the body's trivial records in source order.
func statementPos(s Statement) Pos {
	switch s := s.(type) {
	case *ExecStatement:
		return s.Pos
	case *DDConcatenation:
		return s.Pos
	case *CommentStatement:
		return s.Pos
	case *NullStatement:
		return s.Pos
	case *DelimiterStatement:
		return s.Pos
	default:
		return Pos{}
	}
}

// posLess reports whether a precedes b in source order (by line, then column).
func posLess(a, b Pos) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
}

// writeStatement writes one statement record: "//" name op operands, terminated
// by a newline. The name field is padded so the operation lands in
// operationColumn and the operation field is padded so the operands land in
// operandColumn — the conventional JCL alignment.
func writeStatement(pr *printer, name *Name, op string, params []Parameter) {
	pr.write("//") // columns 1-2; the next character is at column 3.
	text := ""
	if name != nil {
		text = name.Text
	}
	pr.write(text)
	pr.write(spacesTo(3+len(text), operationColumn))
	pr.write(op)
	pr.write(spacesTo(operationColumn+len(op), operandColumn))
	writeParameters(pr, params)
	pr.write("\n")
}

// spacesTo returns the blanks needed to advance to targetCol from nextCol — the
// column the next character would occupy — always at least one so adjacent
// fields stay separated.
func spacesTo(nextCol, targetCol int) string {
	return strings.Repeat(" ", max(targetCol-nextCol, 1))
}

// writeParameters writes a comma-separated parameter field. It is reused for
// both a statement's operands and the items of a subparameter list.
func writeParameters(pr *printer, params []Parameter) {
	for i, param := range params {
		if i > 0 {
			pr.write(",")
		}
		switch p := param.(type) {
		case *PositionalParameter:
			writeValue(pr, p.Value)
		case *KeywordParameter:
			pr.write(p.Name)
			pr.write("=")
			writeValue(pr, p.Value)
		}
	}
}

// writeValue writes a parameter value. It is mutually recursive with
// writeParameters so nested subparameter lists are handled transparently.
func writeValue(pr *printer, v Value) {
	switch v := v.(type) {
	case *Scalar:
		pr.write(v.Text)
	case *QualifiedName:
		for i, seg := range v.Segments {
			if i > 0 {
				pr.write(".")
			}
			pr.write(seg.Text)
		}
	case *QuotedString:
		pr.write(encodeQuotedString(v.Value))
	case *SubparameterList:
		pr.write("(")
		writeParameters(pr, v.Items)
		pr.write(")")
	}
}

// encodeQuotedString is the inverse of decodeQuotedString: it wraps s in
// apostrophes and doubles each embedded apostrophe, undoing the parser's
// collapse of a doubled apostrophe to a single one on decode.
func encodeQuotedString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			b.WriteByte('\'')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('\'')
	return b.String()
}
