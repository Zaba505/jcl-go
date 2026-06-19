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
	// Preamble holds the comment, null, and delimiter statements that appear
	// before the JOB statement, in source order.
	Preamble []Statement

	// Statement is the JOB statement that opens the job. It is nil for the
	// empty-input parse, or for input that holds only preamble statements.
	Statement *JobStatement

	// Body holds the statements that make up the job's steps, in source order:
	// EXEC statements (each carrying its DDs) and the comment, null, and
	// delimiter statements interleaved among them. Later stories add IF and the
	// rest of the body statements.
	Body []Statement
}

// Statement is implemented by the concrete JCL statement AST nodes. The
// unexported marker method keeps the interface closed to this package.
type Statement interface {
	isStatement()
}

func (*JobStatement) isStatement()       {}
func (*ExecStatement) isStatement()      {}
func (*DDConcatenation) isStatement()    {}
func (*CommentStatement) isStatement()   {}
func (*NullStatement) isStatement()      {}
func (*DelimiterStatement) isStatement() {}

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

	// DDs holds the data definitions of this step, in source order, one entry
	// per concatenation group: a named DD and the unnamed DDs that immediately
	// follow it are grouped into a single DDConcatenation. Grouping is purely
	// positional, so a ddname that is coded again later (invalid JCL) opens a
	// second entry rather than merging with the earlier one.
	DDs []*DDConcatenation
}

// DDConcatenation is a step's data definition: a ddname and the one or more
// concatenated DD statements coded under it. The ddname identifies the whole
// concatenation; the second and later members are coded with a blank name field
// in source. A non-concatenated DD is a group of one.
//
// A DDConcatenation is currently produced only as an entry of
// [ExecStatement.DDs] — it is never placed in [Job.Body] — but it implements
// [Statement] so DD can be treated as a body statement by later stories.
type DDConcatenation struct {
	// Pos is the position of the head member's leading "//".
	Pos Pos

	// Name is the ddname, shared by the whole concatenation. It is nil only for
	// an unnamed DD coded with no preceding named DD (invalid JCL the parser
	// tolerates).
	Name *Name

	// DDs holds the member statements in source order; it always has at least
	// one element.
	DDs []*DDStatement
}

// DDStatement is one physical DD record's operands: "//" [name] "DD" parameters.
// The ddname lives on the owning [DDConcatenation], not here, so a member carries
// only its position and parameter field.
type DDStatement struct {
	// Pos is the position of the leading "//".
	Pos Pos

	// Parameters holds the DD operands in source order (DSN, DISP, SPACE, UNIT,
	// VOL, DCB, SYSOUT, DUMMY, …).
	Parameters []Parameter
}

// CommentStatement is a "//*" comment statement record: a whole record of
// free-text commentary. Text is the full record text including the leading
// "//*" — a comment statement is non-semantic and carries nothing to decode, so
// it is recorded verbatim only so the printer can round-trip it.
type CommentStatement struct {
	// Pos is the position of the leading "//*".
	Pos  Pos
	Text string
}

// NullStatement is a "//" null statement record: a bare "//" in columns 1–2 with
// no operation field. It is non-semantic and commonly marks the end of a job.
type NullStatement struct {
	// Pos is the position of the leading "//".
	Pos Pos
}

// DelimiterStatement is a "/*" delimiter statement record. It is non-semantic;
// in context it terminates in-stream data. The optional trailing comment
// ("/*" [ Blank Comments ]) arrives with in-stream data in a later story — only a
// bare "/*" record is parsed here.
type DelimiterStatement struct {
	// Pos is the position of the leading "/*".
	Pos Pos
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
func (*QualifiedName) isValue()    {}
func (*QuotedString) isValue()     {}
func (*SubparameterList) isValue() {}
func (*OmittedValue) isValue()     {}

// Scalar is an unquoted value: a single alphanumeric/national run (a
// [TokenIdentifier]) or a number (a [TokenNumber]). A period-qualified name
// (A.B.C) is a [QualifiedName] whose segments are each a Scalar.
type Scalar struct {
	Pos  Pos
	Text string
}

// QualifiedName is a period-qualified value such as a data set name (A.B.C): the
// assembled form of the grammar's Scalar { "." Scalar }. Segments holds the
// dot-separated parts in order and always has at least two elements — a
// single-segment value is a [Scalar], not a QualifiedName.
type QualifiedName struct {
	// Pos is the position of the first segment.
	Pos      Pos
	Segments []Scalar
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
func (s *DDStatement) appendParameter(p Parameter)      { s.Parameters = append(s.Parameters, p) }
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
	for action := parsePreamble; action != nil && err == nil; {
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

// operationField reads "[Name] Operation" — the statement skeleton after the
// leading "//" has been consumed. The name field begins in column 3 (immediately
// after the "//"); when it is omitted column 3 is blank, so the operation lands in
// a later column. The first identifier at column 3 is therefore the name;
// otherwise it is the operation and there is no name.
func (p *parser) operationField() (name *Name, op Token, err error) {
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

// afterDoubleSlash consumes a leading "//" (at startPos, already peeked by the
// caller) and decides whether the record is a null statement. A null statement is
// a "//" with no operation field: the "//" is not followed by an operation
// identifier — the next token instead starts a new record ("//", "/*", "//*") or
// ends the stream. (In valid JCL every statement record begins with one of those,
// so the only token that can follow a leading "//" within the same record is an
// identifier.) When it is not null, it reads the operation field for the caller to
// dispatch. Exactly one of null / (name, op) is meaningful: null is non-nil for a
// null statement; otherwise op holds the operation.
func (p *parser) afterDoubleSlash(startPos Pos) (null *NullStatement, name *Name, op Token, err error) {
	if _, _, err = p.advance(); err != nil { // consume the "//"
		return nil, nil, Token{}, err
	}
	next, ok, err := p.peek()
	if err != nil {
		return nil, nil, Token{}, err
	}
	if !ok || next.Type != TokenIdentifier {
		return &NullStatement{Pos: startPos}, nil, Token{}, nil
	}
	name, op, err = p.operationField()
	return nil, name, op, err
}

// trivialStatement returns the comment or delimiter statement node that tok
// begins, or nil when tok is neither. The "//" null statement is handled
// separately by afterDoubleSlash, which must consume the "//" to tell a null
// statement from a normal "//" name operation header.
func trivialStatement(tok Token) Statement {
	switch {
	case tok.Type == TokenComment:
		return &CommentStatement{Pos: tok.Pos, Text: string(tok.Value)}
	case isSymbol(tok, "/*"):
		return &DelimiterStatement{Pos: tok.Pos}
	}
	return nil
}

// parsePreamble is the top-level action. It collects the comment, null, and
// delimiter statements that precede the JOB statement into job.Preamble, then
// requires the JOB statement and hands off to parseBody. Empty input — and input
// that holds only preamble statements — parses with a nil Statement.
func parsePreamble(p *parser, job *Job) (parserAction[*Job], error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	if s := trivialStatement(tok); s != nil {
		if _, _, err := p.advance(); err != nil {
			return nil, err
		}
		job.Preamble = append(job.Preamble, s)
		return parsePreamble, nil
	}
	if isSymbol(tok, "//") {
		startPos := tok.Pos
		null, name, op, err := p.afterDoubleSlash(startPos)
		if err != nil {
			return nil, err
		}
		if null != nil {
			job.Preamble = append(job.Preamble, null)
			return parsePreamble, nil
		}
		// The first real statement must be a JOB statement.
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
	return nil, UnexpectedTokenError{Expected: []TokenType{TokenSymbol, TokenComment}, Actual: tok}
}

// parseBody parses the statements that make up the job's steps. It threads the
// current step (the most recent EXEC) through the loop so a DD statement attaches
// to the step it follows; the initial step is nil.
func parseBody(p *parser, job *Job) (parserAction[*Job], error) {
	return parseBodyStatement(nil)(p, job)
}

// parseBodyStatement parses one body statement, then continues with the next. A
// comment, null, or delimiter statement is non-semantic and is appended to the
// body without changing the current step. An EXEC starts a new step and becomes
// the current step for the DDs that follow it; a DD attaches to step, which the
// returned action keeps current. The loop ends when the token stream is
// exhausted.
func parseBodyStatement(step *ExecStatement) parserAction[*Job] {
	return func(p *parser, job *Job) (parserAction[*Job], error) {
		tok, ok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}

		if s := trivialStatement(tok); s != nil {
			if _, _, err := p.advance(); err != nil {
				return nil, err
			}
			job.Body = append(job.Body, s)
			return parseBodyStatement(step), nil
		}
		if isSymbol(tok, "//") {
			startPos := tok.Pos
			null, name, op, err := p.afterDoubleSlash(startPos)
			if err != nil {
				return nil, err
			}
			if null != nil {
				job.Body = append(job.Body, null)
				return parseBodyStatement(step), nil
			}
			switch string(op.Value) {
			case "EXEC":
				stmt := &ExecStatement{Pos: startPos, Name: name}
				if err := parseParameters(p, stmt); err != nil {
					return nil, err
				}
				job.Body = append(job.Body, stmt)
				return parseBodyStatement(stmt), nil
			case "DD":
				if step == nil {
					return nil, MisplacedDDError{Pos: op.Pos}
				}
				dd := &DDStatement{Pos: startPos}
				if err := parseParameters(p, dd); err != nil {
					return nil, err
				}
				attachDD(step, name, startPos, dd)
				return parseBodyStatement(step), nil
			default:
				return nil, UnexpectedOperationError{Pos: op.Pos, Operation: string(op.Value)}
			}
		}
		return nil, UnexpectedTokenError{Expected: []TokenType{TokenSymbol, TokenComment}, Actual: tok}
	}
}

// attachDD groups a parsed DD member into its step's concatenation list. A named
// DD opens a new concatenation; an unnamed DD continues the step's most recent
// concatenation. An unnamed DD coded with no open concatenation opens its own
// nameless group — invalid JCL the parser tolerates rather than rejects.
func attachDD(step *ExecStatement, name *Name, pos Pos, dd *DDStatement) {
	if name == nil && len(step.DDs) > 0 {
		last := step.DDs[len(step.DDs)-1]
		last.DDs = append(last.DDs, dd)
		return
	}
	step.DDs = append(step.DDs, &DDConcatenation{Pos: pos, Name: name, DDs: []*DDStatement{dd}})
}

// endsParameterField reports whether tok (with availability ok) marks the end of
// a statement's parameter field: the start of the next statement record — "//", a
// "/*" delimiter, or a "//*" comment — or the end of the stream. None of these is
// consumed by the parameter loop.
func endsParameterField(tok Token, ok bool) bool {
	return !ok || isSymbol(tok, "//") || isSymbol(tok, "/*") || tok.Type == TokenComment
}

// parseParameters fills the parameter field of a statement (sink) by running the
// parameter-field action loop. The field is delimited by the start of the next
// statement ("//", "/*", or a "//*" comment) or the end of the stream, neither of
// which it consumes.
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
	if endsParameterField(tok, ok) {
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
		} else if ok && isSymbol(next, ".") {
			// A "." after the identifier makes this a positional period-qualified
			// name (A.B.C), the same Scalar { "." Scalar } value parseValue reads.
			qn, err := parseQualifiedName(p, Scalar{Pos: keyword.Pos, Text: string(keyword.Value)})
			if err != nil {
				return nil, err
			}
			return &PositionalParameter{Pos: qn.Pos, Value: qn}, nil
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
		first := Scalar{Pos: scalar.Pos, Text: string(scalar.Value)}
		// A "." immediately after the scalar makes this a period-qualified name
		// (A.B.C); otherwise it is a plain single-segment scalar.
		if tok, ok, err := p.peek(); err != nil {
			return nil, err
		} else if ok && isSymbol(tok, ".") {
			return parseQualifiedName(p, first)
		}
		return &first, nil
	}
}

// parseQualifiedName reads the remaining "." Scalar segments of a period-qualified
// name whose first segment is already read, running the segment action loop. The
// "." separating the first two segments is the next token.
func parseQualifiedName(p *parser, first Scalar) (*QualifiedName, error) {
	qn := &QualifiedName{Pos: first.Pos, Segments: []Scalar{first}}
	var err error
	for action := parseQualifierSegment; action != nil && err == nil; {
		action, err = action(p, qn)
	}
	if err != nil {
		return nil, err
	}
	return qn, nil
}

// parseQualifierSegment consumes one "." plus the scalar that follows it, appends
// the scalar to qn, and continues while another "." follows; it stops when the
// next token is not ".".
func parseQualifierSegment(p *parser, qn *QualifiedName) (parserAction[*QualifiedName], error) {
	tok, ok, err := p.peek()
	if err != nil {
		return nil, err
	}
	if !ok || !isSymbol(tok, ".") {
		return nil, nil
	}
	if _, _, err := p.advance(); err != nil { // consume the "."
		return nil, err
	}
	scalar, err := p.expect(TokenIdentifier, TokenNumber)
	if err != nil {
		return nil, err
	}
	qn.Segments = append(qn.Segments, Scalar{Pos: scalar.Pos, Text: string(scalar.Value)})
	return parseQualifierSegment, nil
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
	case *QualifiedName:
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

// MisplacedDDError is returned when a DD statement appears with no preceding EXEC
// step to own it (a DD before the job's first step). A DD's data definitions
// belong to a step, so such a DD has no owner.
type MisplacedDDError struct {
	Pos Pos
}

// Error implements the [error] interface.
func (e MisplacedDDError) Error() string {
	return fmt.Sprintf("DD statement at line %d, column %d has no preceding step", e.Pos.Line, e.Pos.Column)
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
