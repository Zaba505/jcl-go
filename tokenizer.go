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
	"unicode"
	"unicode/utf8"
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
// unchanged), reporting false at end of input or on any read error. The lexemes
// that need lookahead here — the second '/' of the "//" identifier and the
// doubled apostrophe inside a quoted string — are all ASCII, so a byte is enough.
// [bufio.Reader.Peek] invalidates a subsequent UnreadRune, so a byte peeked here
// must be consumed with [tokenizer.next], never put back with [tokenizer.backup].
func (t *tokenizer) peekByte() (byte, bool) {
	b, _ := t.buf.Peek(1)
	if len(b) == 0 {
		return 0, false
	}
	return b[0], true
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

// skipWhitespace consumes leading whitespace, then runs next.
func skipWhitespace(next tokenizerAction) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				return yieldErrorOr(err, next)
			}
			if !unicode.IsSpace(r) {
				return yieldErrorOr(t.backup(pos), next)
			}
		}
	}
}

// tokenizeJCL is the entry-point action. It skips leading whitespace, then
// dispatches on the next rune to a specific sub-tokenizer.
//
// This slice recognizes the lexemes of a minimal job: the "//" statement
// identifier, name/operation/keyword/value runs (all [TokenIdentifier], the
// parser classifies them by field position), apostrophe-delimited quoted
// strings, and the symbols ( ) , = . A rune that begins no recognized lexeme
// yields an [UnexpectedCharacterError]. Numbers, the . * & + - symbols, //*
// comments, and line continuation are deferred to later stories.
func tokenizeJCL(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
	return skipWhitespace(func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		pos := t.pos
		r, err := t.next()
		if err != nil {
			return yieldErrorOr(err, nil)
		}
		switch {
		case r == '/':
			return tokenizeStatementIdentifier(pos)
		case r == '\'':
			return tokenizeString(pos)
		case r == '(' || r == ')' || r == ',' || r == '=':
			return yieldSymbol(pos, utf8.AppendRune(nil, r))
		case isNameStart(r):
			return tokenizeName(pos, r)
		default:
			yield(Token{}, UnexpectedCharacterError{Pos: pos, Char: r})
			return nil
		}
	})
}

// yieldSymbol emits a single punctuation/operator [TokenSymbol], then resumes
// the top-level dispatch.
func yieldSymbol(pos Pos, value []byte) tokenizerAction {
	return yieldTokenThen(Token{Pos: pos, Type: TokenSymbol, Value: value}, tokenizeJCL)
}

// tokenizeStatementIdentifier scans the "//" statement identifier in columns
// 1–2, the first '/' already consumed at start. Only the "//" form is recognized
// in this slice; the "//*" comment and "/*" delimiter identifiers are deferred,
// so a lone '/' (or a '/' at end of input) is unexpected.
func tokenizeStatementIdentifier(start Pos) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		if b, ok := t.peekByte(); ok && b == '/' {
			_, _ = t.next() // consume the peeked second '/'
			return yieldSymbol(start, []byte("//"))
		}
		yield(Token{}, UnexpectedCharacterError{Pos: start, Char: '/'})
		return nil
	}
}

// tokenizeName accumulates a maximal run of name/value runes (letters, digits,
// national characters), beginning with first already consumed at start. Names,
// operations, keywords, and unquoted values are lexically identical — all emit
// as [TokenIdentifier]; the parser classifies them by field position. A non-name
// rune is backed up so the next action re-reads it; end of input ends the run.
func tokenizeName(start Pos, first rune) tokenizerAction {
	return func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		value := utf8.AppendRune(nil, first)
		for {
			pos := t.pos
			r, err := t.next()
			if err != nil {
				tok := Token{Pos: start, Type: TokenIdentifier, Value: value}
				return yieldTokenThen(tok, yieldErrorOr(err, nil))
			}
			if !isNameContinue(r) {
				tok := Token{Pos: start, Type: TokenIdentifier, Value: value}
				return yieldErrorOr(t.backup(pos), yieldTokenThen(tok, tokenizeJCL))
			}
			value = utf8.AppendRune(value, r)
		}
	}
}

// tokenizeString scans an apostrophe-delimited quoted string, the opening
// apostrophe already consumed at start. Two consecutive apostrophes are an
// escaped apostrophe, not the close. The raw lexeme including both delimiters becomes the
// token value. Reaching end of input first yields an [UnterminatedStringError];
// any other read error propagates verbatim. Continuation across records is
// deferred to a later story.
func tokenizeString(start Pos) tokenizerAction {
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
				if b, ok := t.peekByte(); ok && b == '\'' {
					escaped, _ := t.next()
					value = utf8.AppendRune(value, escaped)
					continue
				}
				tok := Token{Pos: start, Type: TokenString, Value: value}
				return yieldTokenThen(tok, tokenizeJCL)
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
	return isNameStart(r) || ('0' <= r && r <= '9')
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
