// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Parser tests drive the public Parse with real source strings and assert the
// resulting AST, positions included, against a hand-built expected *Job (the
// avro-go/idl and cobol-go parser-test style this package is modeled on). The
// zero-value &Job{} of the empty-input case below is the only expected value
// that is not the result of parsing real source. Positions are copied from the
// matching tokenizer test (the "minimal job" case in tokenizer_test.go).
func TestParser(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		src      string
		expected *Job
	}{
		{
			name:     "empty input parses to empty job",
			src:      "",
			expected: &Job{},
		},
		{
			name: "minimal job",
			src:  "//MYJOB    JOB  (ACCT),'A PROGRAMMER',CLASS=A,MSGCLASS=X\n//STEP1    EXEC PGM=IEFBR14",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "MYJOB"},
					Parameters: []Parameter{
						&PositionalParameter{
							Pos: Pos{Line: 1, Column: 17},
							Value: &SubparameterList{
								Pos: Pos{Line: 1, Column: 17},
								Items: []Parameter{
									&PositionalParameter{
										Pos:   Pos{Line: 1, Column: 18},
										Value: &Scalar{Pos: Pos{Line: 1, Column: 18}, Text: "ACCT"},
									},
								},
							},
						},
						&PositionalParameter{
							Pos:   Pos{Line: 1, Column: 24},
							Value: &QuotedString{Pos: Pos{Line: 1, Column: 24}, Value: "A PROGRAMMER"},
						},
						&KeywordParameter{
							Pos:   Pos{Line: 1, Column: 39},
							Name:  "CLASS",
							Value: &Scalar{Pos: Pos{Line: 1, Column: 45}, Text: "A"},
						},
						&KeywordParameter{
							Pos:   Pos{Line: 1, Column: 47},
							Name:  "MSGCLASS",
							Value: &Scalar{Pos: Pos{Line: 1, Column: 56}, Text: "X"},
						},
					},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "STEP1"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:   Pos{Line: 2, Column: 17},
								Name:  "PGM",
								Value: &Scalar{Pos: Pos{Line: 2, Column: 21}, Text: "IEFBR14"},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := Parse(strings.NewReader(tc.src))
			require.NoError(t, err)
			require.Equal(t, tc.expected, f)
		})
	}
}

func TestParserErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		src    string
		assert func(t *testing.T, err error)
	}{
		{
			name: "first statement is not a job",
			src:  "//STEP1    EXEC PGM=IEFBR14",
			assert: func(t *testing.T, err error) {
				var target UnexpectedOperationError
				require.ErrorAs(t, err, &target)
				require.Equal(t, "EXEC", target.Operation)
				require.Equal(t, Pos{Line: 1, Column: 12}, target.Pos)
			},
		},
		{
			name: "job statement without a name",
			src:  "//   JOB (ACCT)",
			assert: func(t *testing.T, err error) {
				var target MissingNameError
				require.ErrorAs(t, err, &target)
				require.Equal(t, "JOB", target.Operation)
				require.Equal(t, Pos{Line: 1, Column: 6}, target.Pos)
			},
		},
		{
			name: "unsupported body operation",
			src:  "//MYJOB    JOB  (ACCT)\n//SYSIN    DD   DUMMY",
			assert: func(t *testing.T, err error) {
				var target UnexpectedOperationError
				require.ErrorAs(t, err, &target)
				require.Equal(t, "DD", target.Operation)
				require.Equal(t, Pos{Line: 2, Column: 12}, target.Pos)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(strings.NewReader(tc.src))

			require.Error(t, err)
			tc.assert(t, err)
		})
	}
}
