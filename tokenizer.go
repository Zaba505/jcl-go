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
// This is a scaffold: empty (or whitespace-only) input produces no tokens, and
// the dispatch switch is not yet wired up, so any content rune yields an
// [UnexpectedCharacterError]. The implementer replaces the placeholder branch
// with the per-token-class dispatch (comments, identifiers, symbols, strings,
// numbers).
func tokenizeJCL(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
	return skipWhitespace(func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
		pos := t.pos
		r, err := t.next()
		if err != nil {
			return yieldErrorOr(err, nil)
		}
		// TODO: dispatch on r to a sub-tokenizer (see CLAUDE.md). Until then any
		// content rune is unexpected.
		yield(Token{}, UnexpectedCharacterError{Pos: pos, Char: r})
		return nil
	})
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
