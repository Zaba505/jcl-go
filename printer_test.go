// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrinter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    *Job
		expected string
	}{
		{
			name:     "empty job prints nothing",
			input:    &Job{},
			expected: "",
		},
		{
			name: "minimal job",
			input: &Job{
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
			expected: "//MYJOB    JOB  (ACCT),'A PROGRAMMER',CLASS=A,MSGCLASS=X\n//STEP1    EXEC PGM=IEFBR14\n",
		},
		{
			// Locks the O'NEIL -> 'O''NEIL' contract: an apostrophe embedded in a
			// quoted string is re-encoded as a doubled apostrophe.
			name: "quoted string with embedded apostrophe",
			input: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "MYJOB"},
					Parameters: []Parameter{
						&PositionalParameter{
							Pos:   Pos{Line: 1, Column: 17},
							Value: &QuotedString{Pos: Pos{Line: 1, Column: 17}, Value: "O'NEIL"},
						},
					},
				},
			},
			expected: "//MYJOB    JOB  'O''NEIL'\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			err := Print(&buf, tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, buf.String())
		})
	}
}

// TestPrinterRoundTrip pins the Parse -> Print -> Parse -> Equal contract. Every
// printer method added later must have a round-trip case here. Once the printer
// reformats canonically (so positions shift), compare the two ASTs ignoring Pos
// instead of with a plain require.Equal.
func TestPrinterRoundTrip(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		src  string
	}{
		{
			name: "empty source",
			src:  "",
		},
		{
			name: "minimal job",
			src:  "//MYJOB    JOB  (ACCT),'A PROGRAMMER',CLASS=A,MSGCLASS=X\n//STEP1    EXEC PGM=IEFBR14",
		},
		{
			name: "quoted string with embedded apostrophe",
			src:  "//MYJOB    JOB  'O''NEIL'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file1, err := Parse(strings.NewReader(tc.src))
			require.NoError(t, err)

			var buf bytes.Buffer
			require.NoError(t, Print(&buf, file1))

			file2, err := Parse(strings.NewReader(buf.String()))
			require.NoError(t, err)

			require.Equal(t, file1, file2)
		})
	}
}
