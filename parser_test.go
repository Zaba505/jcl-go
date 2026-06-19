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
		{
			name: "exec operands with subparameter lists and numbers",
			src:  "//J JOB\n//S EXEC PGM=IEFBR14,DISP=(NEW,CATLG,DELETE),SPACE=(TRK,(10,5))",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "J"},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "S"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:   Pos{Line: 2, Column: 10},
								Name:  "PGM",
								Value: &Scalar{Pos: Pos{Line: 2, Column: 14}, Text: "IEFBR14"},
							},
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 22},
								Name: "DISP",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 27},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 28}, Value: &Scalar{Pos: Pos{Line: 2, Column: 28}, Text: "NEW"}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 32}, Value: &Scalar{Pos: Pos{Line: 2, Column: 32}, Text: "CATLG"}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 38}, Value: &Scalar{Pos: Pos{Line: 2, Column: 38}, Text: "DELETE"}},
									},
								},
							},
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 46},
								Name: "SPACE",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 52},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 53}, Value: &Scalar{Pos: Pos{Line: 2, Column: 53}, Text: "TRK"}},
										&PositionalParameter{
											Pos: Pos{Line: 2, Column: 57},
											Value: &SubparameterList{
												Pos: Pos{Line: 2, Column: 57},
												Items: []Parameter{
													&PositionalParameter{Pos: Pos{Line: 2, Column: 58}, Value: &Scalar{Pos: Pos{Line: 2, Column: 58}, Text: "10"}},
													&PositionalParameter{Pos: Pos{Line: 2, Column: 61}, Value: &Scalar{Pos: Pos{Line: 2, Column: 61}, Text: "5"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "omitted leading and trailing subparameters",
			src:  "//J JOB\n//S EXEC A=(,CATLG),B=(TRK,)",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "J"},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "S"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 10},
								Name: "A",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 12},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 13}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 13}}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 14}, Value: &Scalar{Pos: Pos{Line: 2, Column: 14}, Text: "CATLG"}},
									},
								},
							},
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 21},
								Name: "B",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 23},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 24}, Value: &Scalar{Pos: Pos{Line: 2, Column: 24}, Text: "TRK"}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 28}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 28}}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "middle and fully omitted subparameters",
			src:  "//J JOB\n//S EXEC A=(NEW,,DELETE),B=(,,)",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "J"},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "S"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 10},
								Name: "A",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 12},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 13}, Value: &Scalar{Pos: Pos{Line: 2, Column: 13}, Text: "NEW"}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 17}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 17}}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 18}, Value: &Scalar{Pos: Pos{Line: 2, Column: 18}, Text: "DELETE"}},
									},
								},
							},
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 26},
								Name: "B",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 28},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 29}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 29}}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 30}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 30}}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 31}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 31}}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "empty subparameter list versus omitted slots",
			src:  "//J JOB\n//S EXEC A=(),B=(,)",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "J"},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "S"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 10},
								Name: "A",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 12},
								},
							},
							&KeywordParameter{
								Pos:  Pos{Line: 2, Column: 15},
								Name: "B",
								Value: &SubparameterList{
									Pos: Pos{Line: 2, Column: 17},
									Items: []Parameter{
										&PositionalParameter{Pos: Pos{Line: 2, Column: 18}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 18}}},
										&PositionalParameter{Pos: Pos{Line: 2, Column: 19}, Value: &OmittedValue{Pos: Pos{Line: 2, Column: 19}}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			// A step with a named-DD concatenation (SYSLIB head + an unnamed
			// continuation), a DUMMY positional, a SYSOUT keyword, and a
			// qualified DSN (X.Y, a QualifiedName). Positions are computed from
			// the source columns; the ddname sits in column 3 and the unnamed
			// continuation's DD does not.
			name: "dd statements attached to step with concatenation",
			src:  "//J JOB\n//S EXEC PGM=P\n//A DD DSN=X.Y,DISP=SHR\n// DD DUMMY\n//B DD SYSOUT=A",
			expected: &Job{
				Statement: &JobStatement{
					Pos:  Pos{Line: 1, Column: 1},
					Name: &Name{Pos: Pos{Line: 1, Column: 3}, Text: "J"},
				},
				Body: []Statement{
					&ExecStatement{
						Pos:  Pos{Line: 2, Column: 1},
						Name: &Name{Pos: Pos{Line: 2, Column: 3}, Text: "S"},
						Parameters: []Parameter{
							&KeywordParameter{
								Pos:   Pos{Line: 2, Column: 10},
								Name:  "PGM",
								Value: &Scalar{Pos: Pos{Line: 2, Column: 14}, Text: "P"},
							},
						},
						DDs: []*DDConcatenation{
							{
								Pos:  Pos{Line: 3, Column: 1},
								Name: &Name{Pos: Pos{Line: 3, Column: 3}, Text: "A"},
								DDs: []*DDStatement{
									{
										Pos: Pos{Line: 3, Column: 1},
										Parameters: []Parameter{
											&KeywordParameter{
												Pos:  Pos{Line: 3, Column: 8},
												Name: "DSN",
												Value: &QualifiedName{
													Pos: Pos{Line: 3, Column: 12},
													Segments: []Scalar{
														{Pos: Pos{Line: 3, Column: 12}, Text: "X"},
														{Pos: Pos{Line: 3, Column: 14}, Text: "Y"},
													},
												},
											},
											&KeywordParameter{
												Pos:   Pos{Line: 3, Column: 16},
												Name:  "DISP",
												Value: &Scalar{Pos: Pos{Line: 3, Column: 21}, Text: "SHR"},
											},
										},
									},
									{
										Pos: Pos{Line: 4, Column: 1},
										Parameters: []Parameter{
											&PositionalParameter{
												Pos:   Pos{Line: 4, Column: 7},
												Value: &Scalar{Pos: Pos{Line: 4, Column: 7}, Text: "DUMMY"},
											},
										},
									},
								},
							},
							{
								Pos:  Pos{Line: 5, Column: 1},
								Name: &Name{Pos: Pos{Line: 5, Column: 3}, Text: "B"},
								DDs: []*DDStatement{
									{
										Pos: Pos{Line: 5, Column: 1},
										Parameters: []Parameter{
											&KeywordParameter{
												Pos:   Pos{Line: 5, Column: 8},
												Name:  "SYSOUT",
												Value: &Scalar{Pos: Pos{Line: 5, Column: 15}, Text: "A"},
											},
										},
									},
								},
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
			name: "dd statement before any step",
			src:  "//MYJOB    JOB  (ACCT)\n//SYSIN    DD   DUMMY",
			assert: func(t *testing.T, err error) {
				var target MisplacedDDError
				require.ErrorAs(t, err, &target)
				require.Equal(t, Pos{Line: 2, Column: 12}, target.Pos)
			},
		},
		{
			name: "unsupported body operation",
			src:  "//MYJOB    JOB  (ACCT)\n//STEP1    EXEC PGM=IEFBR14\n//OUT      OUTPUT CLASS=A",
			assert: func(t *testing.T, err error) {
				var target UnexpectedOperationError
				require.ErrorAs(t, err, &target)
				require.Equal(t, "OUTPUT", target.Operation)
				require.Equal(t, Pos{Line: 3, Column: 12}, target.Pos)
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
