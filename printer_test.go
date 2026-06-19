// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package jcl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		{
			// Pins DD formatting: the ddname aligns the operation/operand fields,
			// an unnamed concatenation continuation is written with a blank name
			// field, and a QualifiedName value is dot-joined. Positions are
			// irrelevant to the printer, so they are left zero.
			name: "step with a dd concatenation",
			input: &Job{
				Statement: &JobStatement{
					Name: &Name{Text: "MYJOB"},
					Parameters: []Parameter{
						&PositionalParameter{
							Value: &SubparameterList{
								Items: []Parameter{
									&PositionalParameter{Value: &Scalar{Text: "ACCT"}},
								},
							},
						},
					},
				},
				Body: []Statement{
					&ExecStatement{
						Name: &Name{Text: "STEP1"},
						Parameters: []Parameter{
							&KeywordParameter{Name: "PGM", Value: &Scalar{Text: "IEBGENER"}},
						},
						DDs: []*DDConcatenation{
							{
								Name: &Name{Text: "SYSLIB"},
								DDs: []*DDStatement{
									{Parameters: []Parameter{
										&KeywordParameter{Name: "DSN", Value: &QualifiedName{Segments: []Scalar{{Text: "MY"}, {Text: "LIB"}, {Text: "A"}}}},
										&KeywordParameter{Name: "DISP", Value: &Scalar{Text: "SHR"}},
									}},
									{Parameters: []Parameter{
										&KeywordParameter{Name: "DSN", Value: &QualifiedName{Segments: []Scalar{{Text: "MY"}, {Text: "LIB"}, {Text: "B"}}}},
										&KeywordParameter{Name: "DISP", Value: &Scalar{Text: "SHR"}},
									}},
								},
							},
							{
								Name: &Name{Text: "SYSPRINT"},
								DDs: []*DDStatement{
									{Parameters: []Parameter{
										&KeywordParameter{Name: "SYSOUT", Value: &Scalar{Text: "A"}},
									}},
								},
							},
						},
					},
				},
			},
			expected: "//MYJOB    JOB  (ACCT)\n" +
				"//STEP1    EXEC PGM=IEBGENER\n" +
				"//SYSLIB   DD   DSN=MY.LIB.A,DISP=SHR\n" +
				"//         DD   DSN=MY.LIB.B,DISP=SHR\n" +
				"//SYSPRINT DD   SYSOUT=A\n",
		},
		{
			// Pins the non-semantic records: a preamble comment is written
			// verbatim (it carries its own "//*"), and a body comment, null
			// statement, and delimiter statement each print as their own record.
			name: "preamble and body trivial statements",
			input: &Job{
				Preamble: []Statement{
					&CommentStatement{Text: "//* JOB HEADER COMMENT"},
				},
				Statement: &JobStatement{
					Name: &Name{Text: "MYJOB"},
					Parameters: []Parameter{
						&PositionalParameter{
							Value: &SubparameterList{
								Items: []Parameter{
									&PositionalParameter{Value: &Scalar{Text: "ACCT"}},
								},
							},
						},
					},
				},
				Body: []Statement{
					&ExecStatement{
						Name: &Name{Text: "STEP1"},
						Parameters: []Parameter{
							&KeywordParameter{Name: "PGM", Value: &Scalar{Text: "IEFBR14"}},
						},
					},
					&CommentStatement{Text: "//* STEP COMMENT"},
					&NullStatement{},
					&DelimiterStatement{},
				},
			},
			expected: "//* JOB HEADER COMMENT\n" +
				"//MYJOB    JOB  (ACCT)\n" +
				"//STEP1    EXEC PGM=IEFBR14\n" +
				"//* STEP COMMENT\n" +
				"//\n" +
				"/*\n",
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
		{
			// Canonical columns so Parse -> Print reproduces the source exactly
			// and the reparsed AST (positions included) equals the first.
			name: "step with a dd concatenation",
			src: "//RT       JOB  (X)\n" +
				"//STEP1    EXEC PGM=IEBGENER\n" +
				"//SYSLIB   DD   DSN=MY.LIB.A,DISP=SHR\n" +
				"//         DD   DSN=MY.LIB.B,DISP=SHR\n" +
				"//SYSPRINT DD   SYSOUT=A",
		},
		{
			name: "comment statement",
			src:  "//J JOB\n//* THIS STEP COMPILES THE PROGRAM",
		},
		{
			name: "null statement",
			src:  "//J JOB\n//",
		},
		{
			name: "delimiter statement",
			src:  "//J JOB\n/*",
		},
		{
			name: "comment statement before the job",
			src:  "//* HEADER COMMENT\n//J JOB",
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

// TestRoundTripFromTestdata is the reusable, fixture-anchored round-trip harness:
// each golden file under testdata/ is parsed, printed, and parsed again, and the
// two ASTs are compared ignoring positions. The printer reformats canonically, so
// positions legitimately shift; cmpopts.IgnoreTypes(Pos{}) drops every Pos-typed
// field, leaving structure and text as the contract. Later fixture-based stories
// add a row to the testCases table — they do not rebuild this harness.
func TestRoundTripFromTestdata(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		fixture string
	}{
		{name: "minimal_job_jcl", fixture: "minimal_job.jcl"},
		{name: "comments_jcl", fixture: "comments.jcl"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			require.NoError(t, err)

			first, err := Parse(bytes.NewReader(data))
			require.NoError(t, err)

			var buf bytes.Buffer
			require.NoError(t, Print(&buf, first))

			second, err := Parse(&buf)
			require.NoError(t, err)

			require.Empty(t, cmp.Diff(first, second, cmpopts.IgnoreTypes(Pos{})))
		})
	}
}
