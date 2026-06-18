// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"fmt"
	"io"
	"iter"
	"slices"
)

// Job is the root of the JCL abstract syntax tree for a parsed job. It is a thin
// container with no position of its own; the nodes below it carry a [Pos],
// mirroring go/ast.
//
// The zero value &Job{} is what the empty-input parse returns. Standalone
// cataloged procedures and INCLUDE groups (a body with no leading JOB) are
// parsed by a separate entry point in a later story and are not modeled here.
type Job struct {
	// Statement is the JOB statement that opens the job. It is nil only for the
	// empty-input parse.
	Statement *JobStatement

	// Body holds the statements that make up the job's steps, in source order.
	// This slice currently holds EXEC statements; later stories add DD, IF, and
	// the rest of the body statements.
	Body []Statement
}

// Statement is implemented by the concrete JCL statement AST nodes. The
// unexported marker method keeps the interface closed to this package.
type Statement interface {
	isStatement()
}

func (*JobStatement) isStatement()  {}
func (*ExecStatement) isStatement() {}

// Name is a name-field label (a jobname or stepname). A keyword or subparameter
// list uses its own representation; only the statement name field is a Name.
type Name struct {
	Pos  Pos
	Text string
}

// JobStatement is a parsed JOB statement: "//" name "JOB" parameters. The name
// (jobname) is required.
type JobStatement struct {
	// Pos is the position of the leading "//".
	Pos Pos

	// Name is the jobname; always non-nil for a parsed JobStatement.
	Name *Name

	// Parameters holds the positional and keyword parameters of the JOB
	// statement in source order (accounting information, programmer name, CLASS,
	// MSGCLASS, …).
	Parameters []Parameter
}

// ExecStatement is a parsed EXEC statement: "//" [name] "EXEC" operands. The
// name (stepname) is optional.
type ExecStatement struct {
	// Pos is the position of the leading "//".
	Pos Pos

	// Name is the stepname, or nil when the EXEC statement has no name.
	Name *Name

	// Parameters holds the EXEC operands in source order. The program to run is
	// carried here as a KeywordParameter (PGM=…), not a dedicated field.
	Parameters []Parameter
}

// Parameter is implemented by the concrete parameter-field entries: a
// positional parameter or a keyword parameter.
type Parameter interface {
	isParameter()
}

func (*PositionalParameter) isParameter() {}
func (*KeywordParameter) isParameter()    {}

// PositionalParameter is a parameter that has no keyword: a bare value or a
// parenthesized subparameter list (e.g. the (ACCT) accounting field or a
// programmer-name string on a JOB statement).
type PositionalParameter struct {
	// Pos is the position where the parameter begins.
	Pos   Pos
	Value Value
}

// KeywordParameter is a keyword=value parameter (e.g. CLASS=A, PGM=IEFBR14).
type KeywordParameter struct {
	// Pos is the position of the keyword token.
	Pos   Pos
	Name  string
	Value Value
}

// Value is implemented by the concrete parameter values: a scalar, a quoted
// string, or a subparameter list.
type Value interface {
	isValue()
}

func (*Scalar) isValue()           {}
func (*QuotedString) isValue()     {}
func (*SubparameterList) isValue() {}
func (*OmittedValue) isValue()     {}

// Scalar is an unquoted value: a single alphanumeric/national run (a
// [TokenIdentifier]) or a number (a [TokenNumber]). Qualified names (A.B.C) are
// a later story.
type Scalar struct {
	Pos  Pos
	Text string
}

// QuotedString is an apostrophe-delimited value. Value is the decoded text: the
// surrounding apostrophes are stripped and each escaped apostrophe — coded as a
// pair of apostrophes in the source — is collapsed to a single one.
type QuotedString struct {
	Pos   Pos
	Value string
}

// SubparameterList is a parenthesized list of parameters, e.g. the (ACCT)
// accounting field. Items may be positional or keyword parameters.
type SubparameterList struct {
	// Pos is the position of the opening "(".
	Pos   Pos
	Items []Parameter
}

// OmittedValue is a null (omitted) positional subparameter: the empty slot
// between adjacent commas in a subparameter list, e.g. the status field of
// DISP=(,KEEP). An omitted slot is still a [PositionalParameter]; its value is
// an OmittedValue rather than a nil Value. Pos is the delimiter ("," or ")")
// that follows the slot.
type OmittedValue struct {
	Pos Pos
}

// parameterSink is anything that accumulates parameters: a statement's parameter
// field or a subparameter list. The parameter-field action loop is written once
// against this interface and reused for both.
type parameterSink interface {
	appendParameter(Parameter)
}

func (s *JobStatement) appendParameter(p Parameter)     { s.Parameters = append(s.Parameters, p) }
func (s *ExecStatement) appendParameter(p Parameter)    { s.Parameters = append(s.Parameters, p) }
func (s *SubparameterList) appendParameter(p Parameter) { s.Items = append(s.Items, p) }

// Parse the JCL source from the given reader into a [Job].
//
// It pulls tokens from [Tokenize] with [iter.Pull2] and runs the top-level
// action loop against a *Job. Empty input parses to the zero-value &Job{}.
func Parse(r io.Reader) (*Job, error) {
	next, stop := iter.Pull2(Tokenize(r))
	defer stop()

	p := &parser{next: next}
	job := &Job{}

	var err error
	for action := parseJob; action != nil && err == nil; {
		action, err = action(p, job)
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

type parser struct {
	// next pulls the next (Token, error) pair from the tokenizer; the final
	// bool reports whether a value was produced (false once exhausted).
	next func() (Token, error, bool)

	// One-token lookahead. The tokenizer skips all whitespace, including
	// newlines, so the parser sees no record boundaries — statement boundaries
	// are the "//" token. Parameter loops therefore peek ahead to tell "another
	// ',' parameter" from "the next '//' statement", which needs the ability to
	// inspect a token without consuming it.
	peeked    Token
	peekedErr error
	peekedOk  bool
	havePeek  bool
}

// peek returns the next token without consuming it. The bool reports whether a
// token was produced (false once the stream is exhausted).
func (p *parser) peek() (Token, bool, error) {
	if !p.havePeek {
		tok, err, ok := p.next()
		p.peeked, p.peekedErr, p.peekedOk = tok, err, ok
		p.havePeek = true
	}
	return p.peeked, p.peekedOk, p.peekedErr
}

// advance returns the next token and consumes it. The bool reports whether a
// token was produced (false once the stream is exhausted).
func (p *parser) advance() (Token, bool, error) {
	if p.havePeek {
		p.havePeek = false
		return p.peeked, p.peekedOk, p.peekedErr
	}
	tok, err, ok := p.next()
	return tok, ok, err
}

// expect consumes the next token and requires its type to be one of types,
// returning [UnexpectedEndOfTokensError] if the stream is exhausted or
// [UnexpectedTokenError] if the type does not match.
func (p *parser) expect(types ...TokenType) (Token, error) {
	tok, ok, err := p.advance()
	if err != nil {
		return Token{}, err
	}
	if !ok {
		return Token{}, UnexpectedEndOfTokensError{Expected: types}
	}
	if !slices.Contains(types, tok.Type) {
		return Token{}, UnexpectedTokenError{Expected: types, Actual: tok}
	}
	return tok, nil
}

// isSymbol reports whether tok is the punctuation/operator symbol s.
func isSymbol(tok Token, s string) bool {
	return tok.Type == TokenSymbol && string(tok.Value) == s
}

// parserAction is one step of the parser state machine, generic over the AST
// node being built. Returning (nil, nil) completes successfully; returning
// (nil, err) terminates with an error — every error path returns nil for the
// next action so the loop stays monotone.
type parserAction[T any] func(p *parser, t T) (parserAction[T], error)

// statementHeader reads the shared statement skeleton "//" [Name] Operation. The
// name field begins in column 3 (immediately after the "//"); when it is omitted
// column 3 is blank, so the operation lands in a later column. The first
// identifier at column 3 is therefore the name; otherwise it is the operation and
// there is no name.
func (p *parser) statementHeader() (name *Name, op Token, err error) {
	id, err := p.expect(TokenSymbol)
	if err != nil {
		return nil, Token{}, err
	}
	if !isSymbol(id, "//") {
		return nil, Token{}, UnexpectedSymbolError{Expected: []string{"//"}, Actual: id}
	}

	op, err = p.expect(TokenIdentifier)
	if err != nil {
		return nil, Token{}, err
	}
	if op.Pos.Column == 3 {
		name = &Name{Pos: op.Pos, Text: string(op.Value)}
		op, err = p.expect(TokenIdentifier)
		if err != nil {
			return nil, Token{}, err
		}
	}
	return name, op, nil
}

// parseJob is the top-level action. Empty input parses to the zero-value &Job{}.
// Otherwise the first statement must be a JOB statement; its body statements are
// parsed by parseBody.
func parseJob(p *parser, job *Job) (parserAction[*Job], error) {
	if _, ok, err := p.peek(); err != nil {
		return nil, err
	} else if !ok {
		return nil, nil
	}

	startPos := mustPeekPos(p)
	name, op, err := p.statementHeader()
	if err != nil {
		return nil, err
	}
	if string(op.Value) != "JOB" {
		return nil, UnexpectedOperationError{Pos: op.Pos, Operation: string(op.Value)}
	}
	if name == nil {
		return nil, MissingNameError{Pos: op.Pos, Operation: "JOB"}
	}

	stmt := &JobStatement{Pos: startPos, Name: name}
	if err := parseParameters(p, stmt); err != nil {
		return nil, err
	}
	job.Statement = stmt
	return parseBody, nil
}

// parseBody parses the statements that make up the job's steps, one per call,
// looping until the token stream is exhausted. Each statement is dispatched on
// its operation field.
func parseBody(p *parser, job *Job) (parserAction[*Job], error) {
	if _, ok, err := p.peek(); err != nil {
		return nil, err
	} else if !ok {
		return nil, nil
	}

	startPos := mustPeekPos(p)
	name, op, err := p.statementHeader()
	if err != nil {
		return nil, err
	}
	switch string(op.Value) {
	case "EXEC":
		stmt := &ExecStatement{Pos: startPos, Name: name}
		if err := parseParameters(p, stmt); err != nil {
			return nil, err
		}
		job.Body = append(job.Body, stmt)
		return parseBody, nil
	default:
		return nil, UnexpectedOperationError{Pos: op.Pos, Operation: string(op.Value)}
	}
}

// mustPeekPos returns the position of the next (already-peeked, available) token.
// Callers must have confirmed a token is available via peek.
func mustPeekPos(p *parser) Pos {
	tok, _, _ := p.peek()
	return tok.Pos
}

// parseParameters fills the parameter field of a statement (sink) by running the
// parameter-field action loop. The field is delimited by the start of the next
// statement ("//") or the end of the stream, neither of which it consumes.
func parseParameters(p *parser, sink parameterSink) error {
	var err error
	for action := parseParameterField; action != nil && err == nil; {
		action, err = action(p, sink)
	}
	return err
}

// parseParameterField reads one parameter into sink, then continues with the next
// parameter when a "," separator follows, or stops at the next statement / end of
// stream. The trailing "//" of the next statement is left unconsumed.
func parseParameterField(p *parser, sink parameterSink) (parserAction[parameterSink], error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	// No operands: a statement may have an empty parameter field, or this is the
	// start of the next statement.
	if !ok || isSymbol(tok, "//") {
		return nil, nil
	}

	param, err := parseParameter(p)
	if err != nil {
		return nil, err
	}
	sink.appendParameter(param)

	tok, ok, err = p.peek()
	if err != nil {
		return nil, err
	}
	if ok && isSymbol(tok, ",") {
		if _, _, err := p.advance(); err != nil { // consume the ","
			return nil, err
		}
		return parseParameterField, nil
	}
	return nil, nil
}

// parseParameter reads a single positional or keyword parameter. A keyword
// parameter is an identifier immediately followed by "="; anything else is a
// positional value.
func parseParameter(p *parser) (Parameter, error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, UnexpectedEndOfTokensError{Expected: []TokenType{TokenIdentifier, TokenNumber, TokenString, TokenSymbol}}
	}

	if tok.Type == TokenIdentifier {
		keyword, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
		if next, ok, err := p.peek(); err != nil {
			return nil, err
		} else if ok && isSymbol(next, "=") {
			if _, _, err := p.advance(); err != nil { // consume the "="
				return nil, err
			}
			value, err := parseValue(p)
			if err != nil {
				return nil, err
			}
			return &KeywordParameter{Pos: keyword.Pos, Name: string(keyword.Value), Value: value}, nil
		}
		return &PositionalParameter{Pos: keyword.Pos, Value: &Scalar{Pos: keyword.Pos, Text: string(keyword.Value)}}, nil
	}

	value, err := parseValue(p)
	if err != nil {
		return nil, err
	}
	return &PositionalParameter{Pos: valuePos(value), Value: value}, nil
}

// parseValue reads a parameter value: a subparameter list, a quoted string, or a
// scalar.
func parseValue(p *parser) (Value, error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, UnexpectedEndOfTokensError{Expected: []TokenType{TokenIdentifier, TokenNumber, TokenString, TokenSymbol}}
	}

	switch {
	case isSymbol(tok, "("):
		return parseSubparameterList(p)
	case tok.Type == TokenString:
		str, err := p.expect(TokenString)
		if err != nil {
			return nil, err
		}
		return &QuotedString{Pos: str.Pos, Value: decodeQuotedString(str.Value)}, nil
	default:
		scalar, err := p.expect(TokenIdentifier, TokenNumber)
		if err != nil {
			return nil, err
		}
		return &Scalar{Pos: scalar.Pos, Text: string(scalar.Value)}, nil
	}
}

// parseSubparameterList reads a parenthesized subparameter list:
// "(" [ Subparameter ] { "," [ Subparameter ] } ")". The opening "(" is the next
// token. An empty list "()" yields zero items; an omitted slot between commas (or
// after the last one) yields an [OmittedValue], so "(,)" yields two omitted items.
func parseSubparameterList(p *parser) (*SubparameterList, error) {
	open, err := p.expect(TokenSymbol)
	if err != nil {
		return nil, err
	}
	if !isSymbol(open, "(") {
		return nil, UnexpectedSymbolError{Expected: []string{"("}, Actual: open}
	}

	list := &SubparameterList{Pos: open.Pos}

	// An immediate ")" is an empty list, distinct from "(,)" (two omitted slots):
	// the empty list has no leading subparameter at all, so it never enters the
	// element loop.
	if tok, ok, err := p.peek(); err != nil {
		return nil, err
	} else if ok && isSymbol(tok, ")") {
		if _, _, err := p.advance(); err != nil { // consume the ")"
			return nil, err
		}
		return list, nil
	}

	var loopErr error
	for action := parseSubparameterElement; action != nil && loopErr == nil; {
		action, loopErr = action(p, list)
	}
	if loopErr != nil {
		return nil, loopErr
	}
	return list, nil
}

// parseSubparameterElement reads one element of a subparameter list — a present
// subparameter or an omitted (null) positional slot — plus its trailing separator,
// then continues on "," or stops on ")". An omitted slot is recognized when the
// element position holds the delimiter itself ("," or ")"); that same token then
// serves as the separator, and the slot is recorded as a [PositionalParameter]
// whose value is an [OmittedValue] positioned at the delimiter.
func parseSubparameterElement(p *parser, list *SubparameterList) (parserAction[*SubparameterList], error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if ok && (isSymbol(tok, ",") || isSymbol(tok, ")")) {
		list.appendParameter(&PositionalParameter{Pos: tok.Pos, Value: &OmittedValue{Pos: tok.Pos}})
	} else {
		param, err := parseParameter(p)
		if err != nil {
			return nil, err
		}
		list.appendParameter(param)
	}

	sep, err := p.expect(TokenSymbol)
	if err != nil {
		return nil, err
	}
	switch {
	case isSymbol(sep, ","):
		return parseSubparameterElement, nil
	case isSymbol(sep, ")"):
		return nil, nil
	default:
		return nil, UnexpectedSymbolError{Expected: []string{",", ")"}, Actual: sep}
	}
}

// valuePos returns the source position of a value, used as the position of a
// positional parameter wrapping it.
func valuePos(v Value) Pos {
	switch v := v.(type) {
	case *Scalar:
		return v.Pos
	case *QuotedString:
		return v.Pos
	case *SubparameterList:
		return v.Pos
	case *OmittedValue:
		return v.Pos
	default:
		return Pos{}
	}
}

// decodeQuotedString decodes a raw quoted-string lexeme (apostrophes included,
// doubled apostrophes for escapes) into its text value: the surrounding
// apostrophes are stripped and each pair of apostrophes is collapsed to a
// single apostrophe.
func decodeQuotedString(raw []byte) string {
	if len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\'' && i+1 < len(raw) && raw[i+1] == '\'' {
			out = append(out, '\'')
			i++
			continue
		}
		out = append(out, raw[i])
	}
	return string(out)
}

// UnexpectedEndOfTokensError is returned when the parser needs another token
// but the stream is exhausted.
type UnexpectedEndOfTokensError struct {
	Expected []TokenType
}

// Error implements the [error] interface.
func (e UnexpectedEndOfTokensError) Error() string {
	return fmt.Sprintf("unexpected end of tokens, expected one of %v", e.Expected)
}

// UnexpectedTokenError is returned when the parser reads a token whose type is
// not one it expected.
type UnexpectedTokenError struct {
	Expected []TokenType
	Actual   Token
}

// Error implements the [error] interface.
func (e UnexpectedTokenError) Error() string {
	return fmt.Sprintf("unexpected token %s at line %d, column %d, expected one of %v", e.Actual, e.Actual.Pos.Line, e.Actual.Pos.Column, e.Expected)
}

// UnexpectedSymbolError is returned when the parser reads a symbol token whose
// value is not one it expected in that position (e.g. a statement that does not
// open with "//", a subparameter list missing its "(", or a list item not
// followed by "," or ")"). Unlike [UnexpectedTokenError], the actual token is a
// symbol; the mismatch is its value, not its type.
type UnexpectedSymbolError struct {
	Expected []string
	Actual   Token
}

// Error implements the [error] interface.
func (e UnexpectedSymbolError) Error() string {
	return fmt.Sprintf("unexpected symbol %q at line %d, column %d, expected one of %v", string(e.Actual.Value), e.Actual.Pos.Line, e.Actual.Pos.Column, e.Expected)
}

// UnexpectedOperationError is returned when a statement's operation field holds
// an operation that is not valid in that position (e.g. a first statement that is
// not JOB, or a body operation the parser does not yet support).
type UnexpectedOperationError struct {
	Pos       Pos
	Operation string
}

// Error implements the [error] interface.
func (e UnexpectedOperationError) Error() string {
	return fmt.Sprintf("unexpected operation %q at line %d, column %d", e.Operation, e.Pos.Line, e.Pos.Column)
}

// MissingNameError is returned when a statement that requires a name field (such
// as JOB) is coded without one.
type MissingNameError struct {
	Pos       Pos
	Operation string
}

// Error implements the [error] interface.
func (e MissingNameError) Error() string {
	return fmt.Sprintf("%s statement requires a name at line %d, column %d", e.Operation, e.Pos.Line, e.Pos.Column)
}
