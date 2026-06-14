# jcl Package - Claude Memory

This file documents the coding style and patterns for the `jcl` package: a text
file library that tokenizes, parses, and formats JCL (mainframe Job Control
Language). It follows a **tokenizer / parser / printer** pipeline, mirroring
`go/scanner` + `go/parser` + `go/printer` in shape but specialized to one
language. The structure and conventions mirror the sibling libraries
`Zaba505/cobol-go` and `z5labs/avro-go/idl`.

```
source ── Tokenize ─► iter.Seq2[Token, error] ── Parse ─► *Job (AST) ── Print ─► source
              │                                     │                       │
         tokenizer.go                           parser.go               printer.go
```

The whole pipeline is a state machine expressed as **recursive action
functions**. Each component has a slightly different action signature, but they
all behave the same way: an action does some work, then returns the next action
to run (or `nil` to stop). A small driver loop calls actions until one returns
`nil`.

> **Status:** this package is a scaffold. The tokenizer, parser, and printer
> compile and pass an empty-input round-trip, but no real tokens or AST nodes
> exist yet. The entry-point actions (`tokenizeJCL`, `parseJob`, `printJob`)
> are stubs marked with `TODO`; fill them in against a `SPEC.md` extracted from
> the JCL reference.

## State Machine Pattern

### Tokenizer Actions

```go
type tokenizerAction func(t *tokenizer, yield func(Token, error) bool) tokenizerAction
```

- Each action reads some runes, optionally calls `yield` to emit a `Token`, and
  returns the next action to execute.
- Return `nil` to end iteration.
- `yield` follows Go iterator conventions: it returns `false` to stop early.

### Parser Actions

```go
type parserAction[T any] func(p *parser, t T) (parserAction[T], error)
```

- Generic over the AST node being built (e.g. `*Job`, and later the concrete
  statement nodes).
- Return `(nil, nil)` to complete successfully.
- Return `(nil, err)` to terminate with an error — every error path returns
  `nil` for the next action so the loop stays monotone.

### Printer Actions

```go
type printerAction func(pr *printer, j *Job) printerAction
```

- Each action writes some output and returns the next action; return `nil` to
  end. There is **no** error return — errors accumulate in `pr.err`, and the
  driver loop stops on the first write failure.

## Tokenizer (`tokenizer.go`)

The tokenizer turns bytes into a lazy stream of `Token` values via
`Tokenize(r io.Reader) iter.Seq2[Token, error]`. The `tokenizer` struct wraps a
`*bufio.Reader` for one-rune lookahead and tracks `Pos{Line, Column}` so every
token knows where it came from. `next()` advances and updates position;
`backup(previousPos Pos)` rewinds the last rune and restores the captured
position.

`Token.Value` is a `[]byte` slice. `TokenType` is a typed int with a `String()`
method — named values pay for themselves the first time a test fails.

### Helpers

- `yieldTokenThen(tok, next)` — yield a token, then continue with `next`. The
  most common ending of an action.
- `yieldErrorOr(err, next)` — continue with `next` on a nil error, terminate
  cleanly at end of input (`io.EOF` or `io.ErrUnexpectedEOF`), otherwise yield
  the error then continue. Use it after any operation that may fail.
- `skipWhitespace(next)` — consume leading whitespace, then run `next`.

### Entry point pattern

`tokenizeJCL` captures the position **before** reading a rune, then dispatches on
that rune to a specific sub-tokenizer:

```go
func tokenizeJCL(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
    return skipWhitespace(func(t *tokenizer, yield func(Token, error) bool) tokenizerAction {
        pos := t.pos
        r, err := t.next()
        if err != nil {
            return yieldErrorOr(err, nil)
        }
        switch {
        // case r == '/': return tokenizeStatement(pos)
        // ... dispatch on r to a sub-tokenizer
        }
    })
}
```

When a sub-tokenizer needs to capture state (the start position of a literal,
accumulated digits), return a **closure** that holds that state rather than
adding per-token fields to the struct.

### Errors

Use a typed error per failure mode (e.g. `UnexpectedCharacterError{Pos, Char}`),
never a bare `fmt.Errorf` in the hot path, so the parser and tests can assert
with `errors.As`.

## Parser (`parser.go`)

`Parse(r io.Reader) (*Job, error)` converts the push-based tokenizer to
pull-based with `iter.Pull2(Tokenize(r))` (`defer stop()`), then runs the
top-level action loop against a `*Job`. `Job` is the root AST node for a parsed
job — a thin container with no position of its own; every node below it carries a
`Pos`, mirroring `go/ast`. (Standalone cataloged procedures parse via a separate
entry point in a later story.)

### `expect`

The `parser` exposes one helper:

```go
tok, err := p.expect(TokenIdentifier, TokenSymbol)
```

It pulls the next token and checks its type against the given types, returning
`UnexpectedEndOfTokensError` if the stream is exhausted or `UnexpectedTokenError`
otherwise. Use it everywhere the grammar requires a specific token; never inline
the type check.

### The inner action loop rule (the one that matters)

For any complex/nested construct, implementations **must** use an inner action
loop, **not** an inline `for` with a `switch`:

```go
func parseThing(p *parser, parent *Parent) (parserAction[*Parent], error) {
    thing := &Thing{}
    var err error
    for action := parseThingHeader; action != nil && err == nil; {
        action, err = action(p, thing)
    }
    if err != nil {
        return nil, err
    }
    parent.Things = append(parent.Things, thing)
    return parseNextThing, nil // dispatch the next thing, or nil to end
}
```

Each state of the construct gets its own `parserAction[T]`. Complex parsers
accrete states; a flat switch becomes unreadable and untestable, while small
named action functions can be exercised directly. **This is the single rule a
fast implementer is most likely to break.**

## Printer (`printer.go`)

`Print(w io.Writer, j *Job) error` runs the action loop, checking `pr.err` each
iteration. The `printer` wraps an `io.Writer` and stores `err error`; every write
goes through `pr.write(s)` or `pr.writef(format, args...)`, which short-circuit
when `pr.err != nil`. Use `writeThen(s, next)` for the common
write-then-continue step.

When printing a slice, use a **closure** that captures the current index and
returns either "print the current element then advance" or `nil` when the index
is past the end — same shape as the tokenizer's closure pattern, no mutable
iterator state on the printer struct.

## Testing Style

- **Table-driven**, with a `testCases` slice and `t.Run(tc.name, ...)`. Names are
  lowercase descriptive.
- `t.Parallel()` at **both** the test function and each subtest. Action functions
  are pure, so parallel tests catch hidden global state.
- Assertions via `github.com/stretchr/testify/require` (not `assert`) — a parser
  test that keeps running after the first failure produces noise.
- Run `go test -race ./...` after every change.

### Tokenizer tests

Source string in, `[]Token` out. A `collect` helper drains the
`iter.Seq2[Token, error]`. Specify **exact** positions for every token — getting
them right early saves debugging later.

### Parser tests

Source string in, `*Job` out via the public `Parse()`. **Drive `Parse()` with
real source strings, then assert the result against a hand-built expected `*Job`
with `require.Equal`** — positions included, in the `avro-go/idl` and `cobol-go`
parser-test style this package is modeled on. Specify exact `Pos` for every node
(copy them from the matching tokenizer test). The empty-input case asserting
`&Job{}` is the one exception to "drive `Parse()` for every expected value".
Failure-path subtests use `require.ErrorAs` for typed errors and
`require.ErrorIs` for sentinels.

### Printer tests

Two shapes, both required for every printer method once real ones exist:

1. **Direct** — explicit `*Job` in, expected string out. Pins down formatting
   (whitespace, punctuation) the round-trip can't see.
2. **Round-trip** — `Parse → Print → Parse`, comparing the two ASTs. The printer
   reformats canonically, so once nodes carry positions, compare **ignoring
   `Pos`** (e.g. via a `withoutPos` helper), as `go/printer` does. The cheapest
   end-to-end correctness check; a mismatch is almost always a parser dropping a
   token or a printer omitting punctuation the parser made optional.

## Why this shape

One format, one package, three files of production code, one action-loop pattern
repeated three times, round-trip tests on every printer method. JCL accretes
constructs; this layout keeps the round-trip property auditable at a glance.
