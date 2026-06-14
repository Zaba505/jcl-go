// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"iter"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenizer(t *testing.T) {
	t.Parallel()

	collect := func(seq iter.Seq2[Token, error]) ([]Token, error) {
		var tokens []Token
		for tok, err := range seq {
			if err != nil {
				return tokens, err
			}
			t.Log(tok)
			tokens = append(tokens, tok)
		}
		return tokens, nil
	}

	testCases := []struct {
		name     string
		src      string
		expected []Token
	}{
		{
			name:     "empty input yields no tokens",
			src:      "",
			expected: nil,
		},
		{
			name:     "whitespace only yields no tokens",
			src:      "  \n\t ",
			expected: nil,
		},
		{
			name: "statement identifier",
			src:  "//",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
			},
		},
		{
			name: "name",
			src:  "MYJOB",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("MYJOB")},
			},
		},
		{
			name: "name with trailing digits",
			src:  "STEP1",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("STEP1")},
			},
		},
		{
			name: "name with national characters",
			src:  "@JOB$1#",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("@JOB$1#")},
			},
		},
		{
			name: "open paren symbol",
			src:  "(",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("(")},
			},
		},
		{
			name: "close paren symbol",
			src:  ")",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "comma symbol",
			src:  ",",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte(",")},
			},
		},
		{
			name: "equals symbol",
			src:  "=",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("=")},
			},
		},
		{
			name: "quoted string",
			src:  "'A PROGRAMMER'",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenString, Value: []byte("'A PROGRAMMER'")},
			},
		},
		{
			name: "empty quoted string",
			src:  "''",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenString, Value: []byte("''")},
			},
		},
		{
			name: "quoted string with escaped apostrophe",
			src:  "'O''NEIL'",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenString, Value: []byte("'O''NEIL'")},
			},
		},
		{
			name: "job statement",
			src:  "//MYJOB    JOB  (ACCT),'A PROGRAMMER',CLASS=A,MSGCLASS=X",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
				{Pos: Pos{Line: 1, Column: 3}, Type: TokenIdentifier, Value: []byte("MYJOB")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenIdentifier, Value: []byte("JOB")},
				{Pos: Pos{Line: 1, Column: 17}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 18}, Type: TokenIdentifier, Value: []byte("ACCT")},
				{Pos: Pos{Line: 1, Column: 22}, Type: TokenSymbol, Value: []byte(")")},
				{Pos: Pos{Line: 1, Column: 23}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 24}, Type: TokenString, Value: []byte("'A PROGRAMMER'")},
				{Pos: Pos{Line: 1, Column: 38}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 39}, Type: TokenIdentifier, Value: []byte("CLASS")},
				{Pos: Pos{Line: 1, Column: 44}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 45}, Type: TokenIdentifier, Value: []byte("A")},
				{Pos: Pos{Line: 1, Column: 46}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 47}, Type: TokenIdentifier, Value: []byte("MSGCLASS")},
				{Pos: Pos{Line: 1, Column: 55}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 56}, Type: TokenIdentifier, Value: []byte("X")},
			},
		},
		{
			name: "bare number",
			src:  "10",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenNumber, Value: []byte("10")},
			},
		},
		{
			name: "keyword parameter with subparameter list",
			src:  "DISP=(NEW,CATLG,DELETE)",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("DISP")},
				{Pos: Pos{Line: 1, Column: 5}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 6}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 7}, Type: TokenIdentifier, Value: []byte("NEW")},
				{Pos: Pos{Line: 1, Column: 10}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 11}, Type: TokenIdentifier, Value: []byte("CATLG")},
				{Pos: Pos{Line: 1, Column: 16}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 17}, Type: TokenIdentifier, Value: []byte("DELETE")},
				{Pos: Pos{Line: 1, Column: 23}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "nested subparameter list with numbers",
			src:  "SPACE=(TRK,(10,5))",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("SPACE")},
				{Pos: Pos{Line: 1, Column: 6}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 7}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 8}, Type: TokenIdentifier, Value: []byte("TRK")},
				{Pos: Pos{Line: 1, Column: 11}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 13}, Type: TokenNumber, Value: []byte("10")},
				{Pos: Pos{Line: 1, Column: 15}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 16}, Type: TokenNumber, Value: []byte("5")},
				{Pos: Pos{Line: 1, Column: 17}, Type: TokenSymbol, Value: []byte(")")},
				{Pos: Pos{Line: 1, Column: 18}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "numeric subparameters",
			src:  "MSGLEVEL=(1,1)",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("MSGLEVEL")},
				{Pos: Pos{Line: 1, Column: 9}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 10}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 11}, Type: TokenNumber, Value: []byte("1")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 13}, Type: TokenNumber, Value: []byte("1")},
				{Pos: Pos{Line: 1, Column: 14}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "omitted leading positional subparameter",
			src:  "DISP=(,CATLG)",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenIdentifier, Value: []byte("DISP")},
				{Pos: Pos{Line: 1, Column: 5}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 6}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 7}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 8}, Type: TokenIdentifier, Value: []byte("CATLG")},
				{Pos: Pos{Line: 1, Column: 13}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "all omitted positional subparameters",
			src:  "(,,)",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 2}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 3}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 4}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "dd statement with subparameter lists",
			src:  "//SYSUT1   DD DISP=(NEW,CATLG,DELETE),SPACE=(TRK,(10,5))",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
				{Pos: Pos{Line: 1, Column: 3}, Type: TokenIdentifier, Value: []byte("SYSUT1")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenIdentifier, Value: []byte("DD")},
				{Pos: Pos{Line: 1, Column: 15}, Type: TokenIdentifier, Value: []byte("DISP")},
				{Pos: Pos{Line: 1, Column: 19}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 20}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 21}, Type: TokenIdentifier, Value: []byte("NEW")},
				{Pos: Pos{Line: 1, Column: 24}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 25}, Type: TokenIdentifier, Value: []byte("CATLG")},
				{Pos: Pos{Line: 1, Column: 30}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 31}, Type: TokenIdentifier, Value: []byte("DELETE")},
				{Pos: Pos{Line: 1, Column: 37}, Type: TokenSymbol, Value: []byte(")")},
				{Pos: Pos{Line: 1, Column: 38}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 39}, Type: TokenIdentifier, Value: []byte("SPACE")},
				{Pos: Pos{Line: 1, Column: 44}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 45}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 46}, Type: TokenIdentifier, Value: []byte("TRK")},
				{Pos: Pos{Line: 1, Column: 49}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 50}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 51}, Type: TokenNumber, Value: []byte("10")},
				{Pos: Pos{Line: 1, Column: 53}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 54}, Type: TokenNumber, Value: []byte("5")},
				{Pos: Pos{Line: 1, Column: 55}, Type: TokenSymbol, Value: []byte(")")},
				{Pos: Pos{Line: 1, Column: 56}, Type: TokenSymbol, Value: []byte(")")},
			},
		},
		{
			name: "exec statement",
			src:  "//STEP1    EXEC PGM=IEFBR14",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
				{Pos: Pos{Line: 1, Column: 3}, Type: TokenIdentifier, Value: []byte("STEP1")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenIdentifier, Value: []byte("EXEC")},
				{Pos: Pos{Line: 1, Column: 17}, Type: TokenIdentifier, Value: []byte("PGM")},
				{Pos: Pos{Line: 1, Column: 20}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 21}, Type: TokenIdentifier, Value: []byte("IEFBR14")},
			},
		},
		{
			name: "minimal job",
			src:  "//MYJOB    JOB  (ACCT),'A PROGRAMMER',CLASS=A,MSGCLASS=X\n//STEP1    EXEC PGM=IEFBR14",
			expected: []Token{
				{Pos: Pos{Line: 1, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
				{Pos: Pos{Line: 1, Column: 3}, Type: TokenIdentifier, Value: []byte("MYJOB")},
				{Pos: Pos{Line: 1, Column: 12}, Type: TokenIdentifier, Value: []byte("JOB")},
				{Pos: Pos{Line: 1, Column: 17}, Type: TokenSymbol, Value: []byte("(")},
				{Pos: Pos{Line: 1, Column: 18}, Type: TokenIdentifier, Value: []byte("ACCT")},
				{Pos: Pos{Line: 1, Column: 22}, Type: TokenSymbol, Value: []byte(")")},
				{Pos: Pos{Line: 1, Column: 23}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 24}, Type: TokenString, Value: []byte("'A PROGRAMMER'")},
				{Pos: Pos{Line: 1, Column: 38}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 39}, Type: TokenIdentifier, Value: []byte("CLASS")},
				{Pos: Pos{Line: 1, Column: 44}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 45}, Type: TokenIdentifier, Value: []byte("A")},
				{Pos: Pos{Line: 1, Column: 46}, Type: TokenSymbol, Value: []byte(",")},
				{Pos: Pos{Line: 1, Column: 47}, Type: TokenIdentifier, Value: []byte("MSGCLASS")},
				{Pos: Pos{Line: 1, Column: 55}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 1, Column: 56}, Type: TokenIdentifier, Value: []byte("X")},
				{Pos: Pos{Line: 2, Column: 1}, Type: TokenSymbol, Value: []byte("//")},
				{Pos: Pos{Line: 2, Column: 3}, Type: TokenIdentifier, Value: []byte("STEP1")},
				{Pos: Pos{Line: 2, Column: 12}, Type: TokenIdentifier, Value: []byte("EXEC")},
				{Pos: Pos{Line: 2, Column: 17}, Type: TokenIdentifier, Value: []byte("PGM")},
				{Pos: Pos{Line: 2, Column: 20}, Type: TokenSymbol, Value: []byte("=")},
				{Pos: Pos{Line: 2, Column: 21}, Type: TokenIdentifier, Value: []byte("IEFBR14")},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := collect(Tokenize(strings.NewReader(tc.src)))
			require.NoError(t, err)
			require.Equal(t, tc.expected, tokens)
		})
	}
}

func TestTokenizerErrors(t *testing.T) {
	t.Parallel()

	collect := func(seq iter.Seq2[Token, error]) error {
		for _, err := range seq {
			if err != nil {
				return err
			}
		}
		return nil
	}

	testCases := []struct {
		name   string
		src    string
		assert func(t *testing.T, err error)
	}{
		{
			name: "unterminated quoted string",
			src:  "'abc",
			assert: func(t *testing.T, err error) {
				var target UnterminatedStringError
				require.ErrorAs(t, err, &target)
				require.Equal(t, Pos{Line: 1, Column: 1}, target.Pos)
			},
		},
		{
			name: "string closed only by an escaped apostrophe",
			src:  "'abc''",
			assert: func(t *testing.T, err error) {
				var target UnterminatedStringError
				require.ErrorAs(t, err, &target)
				require.Equal(t, Pos{Line: 1, Column: 1}, target.Pos)
			},
		},
		{
			name: "unexpected character",
			src:  "?",
			assert: func(t *testing.T, err error) {
				var target UnexpectedCharacterError
				require.ErrorAs(t, err, &target)
				require.Equal(t, '?', target.Char)
				require.Equal(t, Pos{Line: 1, Column: 1}, target.Pos)
			},
		},
		{
			name: "lone slash is not a statement identifier",
			src:  "/",
			assert: func(t *testing.T, err error) {
				var target UnexpectedCharacterError
				require.ErrorAs(t, err, &target)
				require.Equal(t, '/', target.Char)
				require.Equal(t, Pos{Line: 1, Column: 1}, target.Pos)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := collect(Tokenize(strings.NewReader(tc.src)))

			require.Error(t, err)
			tc.assert(t, err)
		})
	}
}
