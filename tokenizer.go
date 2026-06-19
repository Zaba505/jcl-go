// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"iter"
	"unicode/utf8"
)

// JCL is a column-oriented language descended from the 80-column punched card.
// Significant text lives in columns 1-71; column 72 is the continuation
// indicator; columns 73-80 are ignored sequence text. The name field begins in
// column 3, immediately after the "//" identifier.
const (
	nameColumn            = 3
	lastSignificantColumn = 71
	continuationColumn    = 72
	sequenceColumn        = 73
)

// Pos represents the position of a token in the input.
type Pos struct {
	Line   int
	Column int
}

// Token represents a single lexical token in the JCL source.
type Token struct {
	Pos   Pos
	Type  TokenType
	Value []byte
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%s)", t.Type, t.Value)
}

// TokenType represents the type of a [Token].
type TokenType int

const (
	TokenComment    TokenType = iota // a comment
	TokenIdentifier                  // an identifier or keyword
	TokenSymbol                      // a punctuation/operator symbol
	TokenString                      // a string literal
	TokenNumber                      // a numeric literal
)

func (tt TokenType) String() string {
	switch tt {
	case TokenComment:
		return "Comment"
	case TokenIdentifier:
		return "Identifier"
	case TokenSymbol:
		return "Symbol"
	case TokenString:
		return "String"
	case TokenNumber:
		return "Number"
	default:
		panic(fmt.Sprintf("unknown token type: %d", tt))
	}
}

// Tokenize the JCL source defined in the given reader.
//
// Tokens are produced lazily via [iter.Seq2] so the parser can consume one
// token at a time and so errors surface at the position where they occur.
func Tokenize(r io.Reader) iter.Seq2[Token, error] {
	return func(yield func(Token, error) bool) {
		t := &tokenizer{
			pos: Pos{Line: 1, Column: 1},
			buf: bufio.NewReader(r),
		}

		for action := tokenizeJCL; action != nil; {
			action = action(t, yield)
		}
	}
}

type tokenizer struct {
	// pos tracks the current position in the input for error reporting.
	pos Pos

	buf *bufio.Reader
}

// next advances one rune and updates the position.
func (t *tokenizer) next() (rune, error) {
	r, _, err := t.buf.ReadRune()
	if err != nil {
		return 0, err
	}
	t.pos.Column++
	if r == '\n' {
		t.pos.Line++
		t.pos.Column = 1
	}
	return r, nil
}

// backup rewinds the last rune read by next and restores the captured position.
func (t *tokenizer) backup(previousPos Pos) error {
	if err := t.buf.UnreadRune(); err != nil {
		return err
	}
	t.pos = previousPos
	return nil
}

// peekByte returns the next unread byte without consuming it (so the position is
// unchanged). It reports [io.EOF] at end of input and propagates any other read
// error, so callers can surface a genuine I/O failure instead of mistaking it
// for end of input. The lexemes that need lookahead here — the second '/' of the
// "//" identifier and the doubled apostrophe inside a quoted string — are all
// ASCII, so a byte is enough. [bufio.Reader.Peek] invalidates a subsequent
// UnreadRune, so a byte peeked here must be consumed with [tokenizer.next],
// never put back with [tokenizer.backup].
func (t *tokenizer) peekByte() (byte, error) {
	b, err := t.buf.Peek(1)
	if len(b) > 0 {
		return b[0], nil
	}
	if err == nil {
		err = io.EOF
	}
	return 0, err
}

// tokenizerAction is one step of the tokenizer state machine: it reads some
// runes, optionally calls yield to emit a [Token], and returns the next action
// to run. Returning nil ends iteration.
type tokenizerAction func(t *tokenizer, yield func(Token, error) bool) tokenizerAction

// yieldTokenThen yields a token and continues with next.
func yieldTokenThen(tok Token, next tokenizerAction) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		if !yield(tok, nil) {
			return nil
		}
		return next
	}
}

// yieldErrorOr handles error propagation in the tokenizer chain. A nil error
// continues with next; reaching the end of input ([io.EOF] or
// [io.ErrUnexpectedEOF]) terminates the stream cleanly; any other error is
// yielded before continuing (or stops if the consumer stops).
func yieldErrorOr(err error, next tokenizerAction) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		if err == nil {
			return next
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if !yield(Token{}, err) {
			return nil
		}
		return next
	}
}

// fieldPhase tracks which field of a statement the tokenizer is in, so the gap
// between tokens can be classified. It is the minimum field-awareness needed to
// locate the comments-field boundary — the first unquoted, non-parenthesized
// blank after at least one operand. The parser is the authority on field
// structure; this only distinguishes "blank before the first operand" (a field
// separator) from "blank after an operand" (the start of the comments field).
type fieldPhase int

const (
	phaseExpectNameOrOp  fieldPhase = iota // after "//", before the operation
	phaseExpectOp                          // a name was seen; the operation is next
	phaseParamsNoOperand                   // the operation was seen; no operand yet
	phaseParamsOperand                     // at least one operand has been seen
)

// continuation is the closure-carried state the gap-skipper needs to decide
// whether the current logical statement continues onto the next record and where
// in the statement it is. It rides in closures (per the package conventions); the
// tokenizer struct gains no fields for it.
type continuation struct {
	phase        fieldPhase
	parenDepth   int  // open '(' minus ')' so far in the parameter field
	pendingComma bool // last token was an operand ',' awaiting continuation
}

func (c continuation) withoutSignals() continuation {
	c.pendingComma = false
	return c
}

// afterIdentifier advances the phase after a [TokenIdentifier]. An identifier at
// column 3 right after "//" is the name; otherwise the first identifier is the
// operation, the next is the first operand.
func (c continuation) afterIdentifier(startColumn int) continuation {
	switch c.phase {
	case phaseExpectNameOrOp:
		if startColumn == nameColumn {
			c.phase = phaseExpectOp
		} else {
			c.phase = phaseParamsNoOperand
		}
	case phaseExpectOp:
		c.phase = phaseParamsNoOperand
	case phaseParamsNoOperand:
		c.phase = phaseParamsOperand
	}
	return c.withoutSignals()
}

// afterOperand advances into the parameter field for a non-identifier operand
// token (number, string, '(', or an intra-operand symbol).
func (c continuation) afterOperand() continuation {
	if c.phase < phaseParamsOperand {
		c.phase = phaseParamsOperand
	}
	return c.withoutSignals()
}

func (c continuation) afterOpenParen() continuation {
	c = c.afterOperand()
	c.parenDepth++
	return c
}

func (c continuation) afterCloseParen() continuation {
	c = c.afterOperand()
	if c.parenDepth > 0 {
		c.parenDepth--
	}
	return c
}

func (c continuation) afterComma() continuation {
	c = c.withoutSignals()
	c.pendingComma = true
	return c
}

// tokenizeJCL is the entry-point action: it begins a fresh statement and skips
// the gap to its first token.
func tokenizeJCL(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
	return enterGap(continuation{phase: phaseExpectNameOrOp})
}

// enterGap skips the inter-token gap (honoring columns and continuation) and then
// dispatches the next token.
func enterGap(cont continuation) tokenizerAction {
	return skipFieldGap(cont, dispatchToken)
}

// dispatchToken reads the next significant rune and routes it to a sub-tokenizer.
//
// It recognizes the lexemes of a minimal job plus the structure of parameter
// fields: the "//" statement identifier, name/operation/keyword/value runs (all
// [TokenIdentifier], the parser classifies them by field position),
// apostrophe-delimited quoted strings, digit-run numbers ([TokenNumber], used for
// numeric subparameters), and the ( ) , = . * + - symbols. The . separates the
// qualifiers of a data set name (A.B.C); the * (back-reference, in-stream star)
// and the + - signs are single symbols the parser reassembles with their
// neighbors. The & introduces a symbolic parameter (&NAME) and && a
// temporary-data-set / literal-ampersand reference (&&NAME); like the . * + -
// symbols above, each is emitted as a single [TokenSymbol] that the parser
// reassembles with the following name run. A rune that begins no recognized
// lexeme yields an [UnexpectedCharacterError]. The cont describes which statement
// field follows so each emitter can advance it.
func dispatchToken(cont continuation) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		pos := t.pos
		r, err := t.next()
		if err != nil {
			return yieldErrorOr(err, nil)
		}
		switch {
		case r == '/':
			return tokenizeStatementIdentifier(pos)
		case r == '\'':
			return tokenizeString(pos, cont)
		case r == ',':
			return yieldCommaThen(pos, cont)
		case r == '(':
			return yieldSymbol(pos, []byte("("), cont.afterOpenParen())
		case r == ')':
			return yieldSymbol(pos, []byte(")"), cont.afterCloseParen())
		case r == '=' || r == '.' || r == '*' || r == '+' || r == '-':
			return yieldSymbol(pos, utf8.AppendRune(nil, r), cont.afterOperand())
		case r == '&':
			return tokenizeSymbolicParameter(pos, cont)
		case isDigit(r):
			return tokenizeNumber(pos, r, cont)
		case isNameStart(r):
			return tokenizeName(pos, r, cont)
		default:
			yield(Token{}, UnexpectedCharacterError{Pos: pos, Char: r})
			return nil
		}
	}
}

// skipFieldGap consumes the blanks between tokens while honoring columns, decides
// continuation at a record boundary, and emits no tokens. Significant text is in
// columns 1-71; a non-blank in column 72 is the continuation indicator; columns
// 73-80 are ignored sequence text. When the gap ends at the first significant
// rune of the same record it runs next; when it ends a parameter field after an
// operand it starts the comments field; when it crosses a record boundary with a
// continuation signal it reassembles the next record.
func skipFieldGap(cont continuation, next func(continuation) tokenizerAction) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		skippedBlank := false
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				return yieldErrorOr(err, nil)
			}
			switch {
			case r == '\r':
				t.pos = pos // zero-width: keep column accounting honest for CRLF
			case r == '\n':
				if cont.pendingComma {
					return consumeContinuationFraming(cont, next)
				}
				return enterGap(continuation{phase: phaseExpectNameOrOp})
			case r == ' ' || r == '\t':
				if pos.Column <= lastSignificantColumn {
					skippedBlank = true
				}
			case pos.Column >= sequenceColumn:
				// ignored sequence text; discard to the end of the record
			case pos.Column == continuationColumn:
				// A non-blank in column 72 is optional and non-authoritative for
				// operand continuation (the trailing comma is what signals it, per
				// SPEC.md), so it is ignored here. Comment continuation, where the
				// column-72 indicator is required, is handled in tokenizeCommentsField.
			default:
				// A blank outside apostrophes/parentheses, after at least one
				// operand, is the comments-field boundary (SPEC.md). A trailing
				// comma does not suppress it: the comma only signals operand
				// continuation at a record boundary, not when a blank and free
				// text follow it on the same record.
				if skippedBlank && cont.phase == phaseParamsOperand &&
					cont.parenDepth == 0 {
					return yieldErrorOr(t.backup(pos), tokenizeCommentsField(pos, nil))
				}
				return yieldErrorOr(t.backup(pos), next(cont.withoutSignals()))
			}
		}
	}
}

// consumeContinuationFraming silently consumes the "//" in columns 1-2 and the
// blank or '*' in column 3 of a continuation record, plus any further leading
// blanks, then resumes the interrupted field via next. It is lenient: the only
// hard requirement is that columns 1-2 are "//"; a resume column slightly off
// 4-16 is accepted. If columns 1-2 are not "//", the continuation signal was a
// false alarm (e.g. a trailing comma at the end of a job) and the record is
// dispatched as a fresh statement.
func consumeContinuationFraming(cont continuation, next func(continuation) tokenizerAction) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		bs, err := t.buf.Peek(2)
		if err != nil && !errors.Is(err, io.EOF) {
			return yieldErrorOr(err, nil)
		}
		if len(bs) < 2 || bs[0] != '/' || bs[1] != '/' {
			return enterGap(continuation{phase: phaseExpectNameOrOp})
		}
		if _, err := t.next(); err != nil { // consume first '/'
			return yieldErrorOr(err, nil)
		}
		if _, err := t.next(); err != nil { // consume second '/'
			return yieldErrorOr(err, nil)
		}
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				return yieldErrorOr(err, nil)
			}
			switch {
			case r == '\r':
				t.pos = pos
			case r == '\n':
				return consumeContinuationFraming(cont, next)
			case r == ' ' || r == '\t':
				// framing blank before the resume column
			case r == '*' && pos.Column == nameColumn:
				// '*' in column 3 is an allowed continuation marker
			case pos.Column >= sequenceColumn:
				// nothing significant before the sequence area
			default:
				return yieldErrorOr(t.backup(pos), next(cont.withoutSignals()))
			}
		}
	}
}

// yieldSymbol emits a single punctuation/operator [TokenSymbol], then resumes the
// gap with the updated continuation state.
func yieldSymbol(pos Pos, value []byte, cont continuation) tokenizerAction {
	return yieldTokenThen(Token{Pos: pos, Type: TokenSymbol, Value: value}, enterGap(cont))
}

// yieldCommaThen emits the ',' [TokenSymbol] and arms the operand-continuation
// signal. If the next significant content is on the same record (more operands),
// the signal is discarded; if a record boundary is reached first, it triggers
// continuation of the parameter field.
func yieldCommaThen(pos Pos, cont continuation) tokenizerAction {
	tok := Token{Pos: pos, Type: TokenSymbol, Value: []byte(",")}
	return yieldTokenThen(tok, enterGap(cont.afterComma()))
}

// tokenizeStatementIdentifier dispatches on the rune following the first '/'
// (already consumed at start) to one of the three identifiers that begin with a
// slash: "//" (statement identifier, or — with nothing after it on the record —
// a null statement), "//*" (a comment statement, scanned whole as one
// [TokenComment] by [tokenizeComment]), and "/*" (a delimiter statement). This
// slice does not enforce column positioning, so each is recognized wherever a
// lexeme may begin; column-1–2 enforcement is deferred. A lone '/' — followed by
// neither '/' nor '*' (including at end of input) — is unexpected. A non-EOF
// read error from the lookahead is propagated.
func tokenizeStatementIdentifier(start Pos) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		b, err := t.peekByte()
		if err != nil && !errors.Is(err, io.EOF) {
			return yieldErrorOr(err, nil)
		}
		switch {
		case err == nil && b == '/':
			if _, err := t.next(); err != nil { // consume the peeked second '/'
				return yieldErrorOr(err, nil)
			}
			return tokenizeAfterDoubleSlash(start)
		case err == nil && b == '*':
			if _, err := t.next(); err != nil { // consume the peeked '*'
				return yieldErrorOr(err, nil)
			}
			return yieldSymbol(start, []byte("/*"), continuation{phase: phaseExpectNameOrOp})
		default:
			yield(Token{}, UnexpectedCharacterError{Pos: start, Char: '/'})
			return nil
		}
	}
}

// tokenizeAfterDoubleSlash decides between the "//" statement identifier and the
// "//*" comment statement, both leading slashes already consumed at start. A '*'
// next continues with [tokenizeComment]; anything else (a name, a blank, or end
// of input) emits the bare "//" [TokenSymbol] and resumes top-level dispatch — a
// lone "//" on a record is a null statement, which the parser recognizes by the
// absence of a following operation. A non-EOF read error from the lookahead is
// propagated.
func tokenizeAfterDoubleSlash(start Pos) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		b, err := t.peekByte()
		if err != nil && !errors.Is(err, io.EOF) {
			return yieldErrorOr(err, nil)
		}
		if err == nil && b == '*' {
			if _, err := t.next(); err != nil { // consume the peeked '*'
				return yieldErrorOr(err, nil)
			}
			return tokenizeComment(start)
		}
		return yieldSymbol(start, []byte("//"), continuation{phase: phaseExpectNameOrOp})
	}
}

// tokenizeSymbolicParameter handles the '&' that introduces a symbolic parameter,
// the first '&' already consumed at start. A second '&' makes it the '&&'
// temporary-data-set / literal-ampersand introducer, emitted as a distinct "&&"
// [TokenSymbol]; otherwise the lone '&' is emitted as "&". Either way the name run
// that follows is left to [tokenizeName] (a [TokenIdentifier]); the parser
// reassembles the symbolic parameter from the symbol and the name. The peeked
// second '&' is consumed with [tokenizer.next], never put back, per the peekByte
// contract. A non-EOF read error from the lookahead is propagated; end of input
// leaves a lone '&'.
func tokenizeSymbolicParameter(start Pos, cont continuation) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		b, err := t.peekByte()
		if err != nil && !errors.Is(err, io.EOF) {
			return yieldErrorOr(err, nil)
		}
		if err == nil && b == '&' {
			if _, err := t.next(); err != nil { // consume the peeked second '&'
				return yieldErrorOr(err, nil)
			}
			return yieldSymbol(start, []byte("&&"), cont.afterOperand())
		}
		return yieldSymbol(start, []byte("&"), cont.afterOperand())
	}
}

// tokenizeComment scans a "//*" comment statement, the "//*" already consumed at
// start, and emits the whole record — from "//*" through the text before the
// record boundary — as one [TokenComment]. The comment carries no syntactic
// content, so its runes are accumulated verbatim rather than tokenized. A '\r'
// is zero-width and excluded from the value so CRLF and LF inputs yield the same
// token; the terminating newline is backed up so the gap skipper consumes it;
// end of input ends the comment. (The comment statement may run to column 80,
// the one exception to the column-71 rule, and cannot be continued.)
func tokenizeComment(start Pos) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		value := []byte("//*")
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				tok := Token{Pos: start, Type: TokenComment, Value: value}
				return yieldTokenThen(tok, yieldErrorOr(err, nil))
			}
			if r == '\r' {
				t.pos = pos // zero-width: keep CR out of the value (CRLF == LF)
				continue
			}
			if r == '\n' {
				tok := Token{Pos: start, Type: TokenComment, Value: value}
				return yieldErrorOr(t.backup(pos), yieldTokenThen(tok, tokenizeJCL))
			}
			value = utf8.AppendRune(value, r)
		}
	}
}

// tokenizeName accumulates a maximal run of name/value runes (letters, digits,
// national characters), beginning with first already consumed at start. Names,
// operations, keywords, and unquoted values are lexically identical — all emit
// as [TokenIdentifier]; the parser classifies them by field position. A non-name
// rune, or a rune past the last significant column (71), ends the run and is
// backed up so the gap skipper re-reads it; end of input ends the run.
func tokenizeName(start Pos, first rune, cont continuation) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		value := utf8.AppendRune(nil, first)
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				tok := Token{Pos: start, Type: TokenIdentifier, Value: value}
				return yieldTokenThen(tok, yieldErrorOr(err, nil))
			}
			if !isNameContinue(r) || pos.Column > lastSignificantColumn {
				tok := Token{Pos: start, Type: TokenIdentifier, Value: value}
				return yieldErrorOr(t.backup(pos), yieldTokenThen(tok, enterGap(cont.afterIdentifier(start.Column))))
			}
			value = utf8.AppendRune(value, r)
		}
	}
}

// tokenizeNumber accumulates a maximal run of decimal digits, beginning with
// first already consumed at start, and emits a [TokenNumber]. JCL numbers are
// plain unsigned decimal integers (return codes, space quantities, generation
// numbers); signed forms combine a sign [TokenSymbol] with a Number and are
// deferred. A non-digit rune, or a rune past the last significant column (71),
// ends the run and is backed up so the next action re-reads it; end of input
// ends the run.
//
// A value that begins with a digit but continues with letters (e.g. a device
// address such as 0A80) splits into a Number followed by an Identifier; this
// mixed-scalar case is not yet handled and is deferred to value tokenization.
func tokenizeNumber(start Pos, first rune, cont continuation) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		value := utf8.AppendRune(nil, first)
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				tok := Token{Pos: start, Type: TokenNumber, Value: value}
				return yieldTokenThen(tok, yieldErrorOr(err, nil))
			}
			if !isDigit(r) || pos.Column > lastSignificantColumn {
				tok := Token{Pos: start, Type: TokenNumber, Value: value}
				return yieldErrorOr(t.backup(pos), yieldTokenThen(tok, enterGap(cont.afterOperand())))
			}
			value = utf8.AppendRune(value, r)
		}
	}
}

// tokenizeString scans an apostrophe-delimited quoted string, the opening
// apostrophe already consumed at start. Two consecutive apostrophes are an
// escaped apostrophe, not the close. The raw lexeme including both delimiters becomes the
// token value. Reaching end of input first yields an [UnterminatedStringError];
// any other read error propagates verbatim. Continuation of an apostrophe-enclosed
// value across records (extend to column 71, resume in column 16) is deferred to a
// later story.
func tokenizeString(start Pos, cont continuation) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		value := []byte{'\''}
		for {
			r, err := t.next()
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					err = UnterminatedStringError{Pos: start}
				}
				yield(Token{}, err)
				return nil
			}
			value = utf8.AppendRune(value, r)
			if r == '\'' {
				if b, err := t.peekByte(); err == nil && b == '\'' {
					escaped, err := t.next() // consume the peeked second '\''
					if err != nil {
						yield(Token{}, err)
						return nil
					}
					value = utf8.AppendRune(value, escaped)
					continue
				}
				tok := Token{Pos: start, Type: TokenString, Value: value}
				return yieldTokenThen(tok, enterGap(cont.afterOperand()))
			}
		}
	}
}

// tokenizeCommentsField scans the comments field trailing a statement's
// parameter field, the first significant rune already at start, and emits the
// free text through column 71 as one [TokenComment]. A non-blank in column 72
// continues the comment onto the next "//" record (see [continueComment]): the
// resumed text is appended to the same token, so a continued comments field is
// one logical [TokenComment]. Columns 73-80 are ignored. End of the record
// without a column-72 indicator, or end of input, ends the comment.
func tokenizeCommentsField(start Pos, value []byte) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		flagged := false
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				tok := Token{Pos: start, Type: TokenComment, Value: value}
				return yieldTokenThen(tok, yieldErrorOr(err, nil))
			}
			switch {
			case r == '\r':
				t.pos = pos
			case r == '\n':
				if flagged {
					return continueComment(start, value)
				}
				tok := Token{Pos: start, Type: TokenComment, Value: value}
				return yieldTokenThen(tok, tokenizeJCL)
			case pos.Column >= sequenceColumn:
				// ignored sequence text
			case pos.Column == continuationColumn:
				if r != ' ' && r != '\t' {
					flagged = true // a non-blank in column 72 continues the comment
				}
			default:
				value = utf8.AppendRune(value, r)
			}
		}
	}
}

// continueComment resumes a comments field on the next record after a column-72
// continuation indicator. It consumes the "//" framing and the leading blanks
// after column 3, then appends the resumed text via [tokenizeCommentsField]. If
// the next record is not a "//" continuation, the accumulated comment is emitted
// and the record is dispatched as a fresh statement.
func continueComment(start Pos, value []byte) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		emit := func(next tokenizerAction) tokenizerAction {
			tok := Token{Pos: start, Type: TokenComment, Value: value}
			return yieldTokenThen(tok, next)
		}
		bs, err := t.buf.Peek(3)
		if err != nil && !errors.Is(err, io.EOF) {
			return yieldErrorOr(err, nil)
		}
		// Comment continuation requires "//" in columns 1-2 and a blank in
		// column 3 (SPEC.md, "Continuing the comments field"). Any other record
		// — including one whose column 3 is non-blank, e.g. "//C EXEC ..." — is a
		// new statement, not a continuation: emit the accumulated comment and
		// dispatch the record fresh rather than swallowing it into the comment.
		if len(bs) < 3 || bs[0] != '/' || bs[1] != '/' || (bs[2] != ' ' && bs[2] != '\t') {
			return emit(tokenizeJCL)
		}
		if _, err := t.next(); err != nil { // consume first '/'
			return emit(yieldErrorOr(err, nil))
		}
		if _, err := t.next(); err != nil { // consume second '/'
			return emit(yieldErrorOr(err, nil))
		}
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				return emit(yieldErrorOr(err, nil))
			}
			switch {
			case r == '\r':
				t.pos = pos
			case r == '\n':
				return continueComment(start, value)
			case r == ' ' || r == '\t':
				// leading framing blank before the resume column
			case pos.Column >= sequenceColumn:
				// nothing significant before the sequence area
			default:
				return yieldErrorOr(t.backup(pos), tokenizeCommentsField(start, value))
			}
		}
	}
}

// isNameStart reports whether r may begin a JCL name or unquoted value: an ASCII
// letter or a national character (@ $ #). Digits begin numbers, not names.
func isNameStart(r rune) bool {
	return ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') || r == '@' || r == '$' || r == '#'
}

// isNameContinue reports whether r may appear after the first rune of a JCL name
// or unquoted value: a letter, digit, or national character.
func isNameContinue(r rune) bool {
	return isNameStart(r) || isDigit(r)
}

// isDigit reports whether r is an ASCII decimal digit, which begins a Number.
func isDigit(r rune) bool {
	return '0' <= r && r <= '9'
}

// UnexpectedCharacterError is returned by the tokenizer when it encounters a
// character that no action expected.
type UnexpectedCharacterError struct {
	Pos  Pos
	Char rune
}

// Error implements the [error] interface.
func (e UnexpectedCharacterError) Error() string {
	return fmt.Sprintf("unexpected character '%c' at line %d, column %d", e.Char, e.Pos.Line, e.Pos.Column)
}

// UnterminatedStringError is returned when a quoted string is not closed before
// the end of input.
type UnterminatedStringError struct {
	Pos Pos
}

// Error implements the [error] interface.
func (e UnterminatedStringError) Error() string {
	return fmt.Sprintf("unterminated string starting at line %d, column %d", e.Pos.Line, e.Pos.Column)
}
