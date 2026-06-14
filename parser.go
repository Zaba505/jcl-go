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

// File is the root of the JCL abstract syntax tree. It is a thin container with
// no position of its own; the nodes below it carry a [Pos].
//
// This is a scaffold: the concrete child nodes (jobs, statements, …) are added
// by the implementer. The zero value &File{} is what the empty-input parse
// returns.
type File struct {
	// Statements holds the top-level statements of a JCL file in source order.
	// It is a placeholder until the concrete AST nodes are implemented.
	Statements []Type
}

// Type is implemented by the concrete JCL AST nodes. The unexported marker
// method keeps the interface closed to this package.
type Type interface {
	isType()
}

// Parse the JCL source from the given reader into a [File].
//
// It pulls tokens from [Tokenize] with [iter.Pull2] and runs the top-level
// action loop against a *File.
func Parse(r io.Reader) (*File, error) {
	next, stop := iter.Pull2(Tokenize(r))
	defer stop()

	p := &parser{next: next}
	f := &File{}

	var err error
	for action := parseFile; action != nil && err == nil; {
		action, err = action(p, f)
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

type parser struct {
	// next pulls the next (Token, error) pair from the tokenizer; the final
	// bool reports whether a value was produced (false once exhausted).
	next func() (Token, error, bool)
}

// expect pulls the next token and requires its type to be one of types,
// returning [UnexpectedEndOfTokensError] if the stream is exhausted or
// [UnexpectedTokenError] if the type does not match.
func (p *parser) expect(types ...TokenType) (Token, error) {
	tok, err, ok := p.next()
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

// parserAction is one step of the parser state machine, generic over the AST
// node being built. Returning (nil, nil) completes successfully; returning
// (nil, err) terminates with an error — every error path returns nil for the
// next action so the loop stays monotone.
type parserAction[T any] func(p *parser, t T) (parserAction[T], error)

// parseFile is the top-level action. It is a scaffold: empty input parses to
// the zero-value *File, so it completes immediately. The implementer wires the
// dispatch on the first token here.
func parseFile(p *parser, f *File) (parserAction[*File], error) {
	return nil, nil
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
