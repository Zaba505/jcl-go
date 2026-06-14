# JCL Specification Reference

## Overview

This document is the single source of truth for the `jcl` package — a
tokenizer / parser / printer pipeline for z/OS **Job Control Language (JCL)**.
It distills the lexical and syntactic rules of JCL into a form each pipeline
stage can be built against: the tokenizer classifies bytes into `Token` values,
the parser builds an AST of statements, and the printer renders that AST back to
source.

JCL is a **line- and column-oriented** language descended from the 80-column
punched card. Every statement is a record of up to 80 bytes; the meaningful text
lives in columns 1–71, a continuation indicator may appear in column 72, and
columns 73–80 are ignored. The language is a flat stream of *statements* — there
is no nesting of source layout, only logical grouping (a job's steps, an
IF/THEN/ELSE/ENDIF construct, a PROC/PEND body). This reference covers the
**core** of that stream: the lexical surface (the statement identifier, the five
fields, character sets, literals, symbolic parameters) and the structure of the
core statements. The exhaustive catalog of keyword parameters for each statement
(every DD, EXEC, and JOB keyword) is deliberately **out of scope** — it is left
to the story that implements each statement; this document specifies the
*general* parameter grammar those keywords plug into.

**Governing source**

- **IBM z/OS MVS JCL Reference**, SA23-1385 (z/OS V2R4, IBM document
  `ieab600`). This is the authoritative reference for the JCL language; section
  and table references below (e.g. *Table 8. JCL Statement Fields*, *Chapter 3.
  Format of statements*, *Chapter 18. IF/THEN/ELSE/ENDIF*) refer to it.
  <https://www.ibm.com/docs/en/SSLTBW_2.4.0/pdf/ieab600_v2r4.pdf>
- The companion **IBM z/OS MVS JCL User's Guide**, SA23-1386, is the
  task-oriented narrative for the same language.

> **Implementation status:** The package is a scaffold (compiling action-loop
> machinery, a seed `TokenType` enum, an empty-input round-trip). No real tokens
> or AST nodes exist yet. A minimal one-step job (JOB + EXEC) is the first
> vertical slice; statement and parameter breadth follow in batches. Cataloged
> procedures and INCLUDE groups stored in separate files are parsed standalone
> and resolved into an effective job later (`ParseProc` / `Expand`); their
> resolution detail is summarized in [Semantics](#semantics) but not fully
> specified here.

**Grammar notation.** Productions are written in **EBNF**: `=` defines a
production, `|` separates alternatives, `[ … ]` marks an optional element,
`{ … }` marks zero-or-more repetition, `( … )` groups, and terminals are either
quoted (`"//"`) or named token classes in PascalCase (`Name`, `QuotedString`).
The literal *blank* (one or more spaces, the field delimiter) is written
`Blank`. Production names are kept close to the IBM field and statement names so
they map onto AST types.

**Stable terminology** (used consistently throughout):

| Term | Meaning |
|---|---|
| **statement** | One logical JCL statement: an identifier `//` plus its fields, possibly spanning continuation records. |
| **record** | One physical 80-byte line. A statement is one or more records. |
| **field** | One of the five parts of a statement: *identifier*, *name*, *operation*, *parameter* (a.k.a. *operand*), *comments*. |
| **operation** | The statement verb in the operation field: `JOB`, `EXEC`, `DD`, `PROC`, `PEND`, `OUTPUT`, `SET`, `INCLUDE`, `JCLLIB`, `IF`, `ELSE`, `ENDIF`. |
| **parameter** | A positional or keyword entry in the parameter field. |
| **subparameter** | A parameter nested inside a parenthesized list. |
| **symbolic parameter** | A `&name` (or `&&name`) substitution variable. |

JCL is **case-insensitive in spirit but conventionally uppercase**: operations,
keywords, and the character sets are uppercase; source is overwhelmingly written
in capitals (a punched-card legacy). See [Keywords and Reserved
Words](#keywords-and-reserved-words) and [Character Encoding](#character-encoding).

## Lexical Elements (Tokens)

The tokenizer turns a byte stream into a lazy sequence of `Token` values. JCL's
lexical structure is **column-aware**: the tokenizer must first recognize the
identifier field in columns 1–2 (or 1–3), then scan the remaining fields in
"free form" up to column 71, honoring the column-72 continuation indicator and
ignoring columns 73–80. Every byte of a well-formed statement (once the column
areas are accounted for) belongs to exactly one of the token classes below.

The package seeds a `TokenType` enum (`Comment`, `Identifier`, `Symbol`,
`String`, `Number`); the classes below are the authoritative lexical classes and
note the seed value each maps to.

| Token class | Maps to seed `TokenType` | Role |
|---|---|---|
| `StatementIdentifier` | `TokenSymbol` | `//`, `//*`, `/*`, or a DLM-defined delimiter in columns 1–2(–3) |
| `Name` | `TokenIdentifier` | name-field label: jobname, stepname, ddname, procname, IF/ELSE/ENDIF label |
| `Operation` | `TokenIdentifier` | statement verb: `JOB`, `EXEC`, `DD`, … (see [Keywords](#keywords-and-reserved-words)) |
| `Keyword` | `TokenIdentifier` | parameter / subparameter keyword (`PGM`, `DSN`, `DISP`, `MEMBER`, …) and IF keywords (`THEN`, `RC`, `ABEND`) |
| `Value` | `TokenIdentifier` | an unquoted operand value (program name, disposition, device type, qualified data set name) |
| `Number` | `TokenNumber` | an unquoted numeric value (return code, space quantity) |
| `QuotedString` | `TokenString` | an apostrophe-delimited literal `'…'` |
| `SymbolicParameter` | *(new)* | `&name` JCL/system symbol, or `&&name` temporary-data-set / literal-ampersand form |
| `Comment` | `TokenComment` | a `//*` comment statement, or the comments field trailing the parameter field |
| `Symbol` | `TokenSymbol` | the syntactic punctuation and operators: `, = ( ) .` and the IF operators `> < >= <= ¬ & \|` |

> **Ambiguity:** Whether `Name`, `Operation`, `Keyword`, and `Value` are
> distinct token types or a single `Identifier` class disambiguated by field
> position is an implementation choice. They are all lexically the same thing —
> a run of characters from the alphanumeric/national/special set — and all map
> to the seed `TokenIdentifier`. This reference names them separately because
> the *parser* recognizes them by position (operation follows the identifier and
> name; a `Keyword` precedes an `=`; a `Value` follows it). An implementation
> may emit one `Identifier` token and let the parser classify, or emit the
> refined classes from the tokenizer. Likewise `SymbolicParameter` may be
> tokenized as a `&`/`&&` `Symbol` followed by a `Name`, or scanned whole; this
> reference treats it as one class because the trailing-period delimiter rule
> (below) is easier to apply in the tokenizer.

### Character sets

JCL source is built from four character sets (*Table 10. Character Sets*):

| Set | Members |
|---|---|
| **Alphanumeric** | `A`–`Z` (capitals), `0`–`9` |
| **National** | `@` (X'7C'), `$` (X'5B'), `#` (X'7B') |
| **Special** | `, . / ' ( ) * & + - =` and the blank |
| **EBCDIC text** | any printable EBCDIC character X'40'–X'FE' (allowed inside apostrophe-enclosed values) |

A *name* and most unquoted values are composed of alphanumeric and national
characters; special characters that have a syntactic function (below) must be
enclosed in apostrophes when they appear *in* a value.

> **Ambiguity:** The reference is defined over EBCDIC code points (e.g. `@` is
> X'7C'). JCL source files handled by this package are read as bytes/runes from
> an `io.Reader`, normally ASCII/UTF-8. This document treats the *glyphs*
> (`@ $ #`, etc.) as the lexical units; an implementation maps incoming bytes to
> those glyphs and need not model EBCDIC code points. See [Character
> Encoding](#character-encoding).

### Comments

JCL has two comment forms:

- **Comment statement** — a record whose columns 1–3 contain `//*`. The rest of
  the record (through column **80**, not 71 — the comment statement is the one
  exception to the column-71 rule) is free-text commentary with no syntactic
  content. Token class `Comment` (seed `TokenComment`).
  - Example: `//* THIS STEP COMPILES THE PROGRAM`
- **Comments field** — free text in the *comments field* of an ordinary
  statement, following the parameter field and at least one blank, through
  column 71. Also token class `Comment`.
  - Example: the `LOAD THE TABLE` in `//STEP1 EXEC PGM=IEFBR14  LOAD THE TABLE`

Comments do **not** nest, do not start a new logical construct, and carry no
semantics (see [Semantics](#semantics)). A `//*` inside an apostrophe-enclosed
value is literal text, not a comment.

> **Ambiguity:** Distinguishing the comments field from the parameter field
> requires knowing where the parameter field ends — the first unquoted blank
> after the operands. Because a comment may follow even a *continued* parameter
> field, the tokenizer must treat "blank outside apostrophes/parentheses, after
> at least one operand" as the comments-field boundary. The reference states the
> rule prose-only (*Chapter 3, Comments field*); the implementation must encode
> it.

### Whitespace and Delimiters

- **The blank is the field delimiter.** At least one blank (`U+0020`) separates
  the identifier+name from the operation, the operation from the parameter
  field, and the parameter field from the comments field (*Chapter 3, Location
  of fields*). One or more blanks act identically.
- **Free-form fields.** Except the identifier (column 1) and name (begins in
  column 3, immediately after the identifier with no intervening blank), the
  operation, parameter, and comments fields do not begin in any fixed column —
  they are positioned only relative to one another by blanks.
- **No blanks inside the parameter field** except where enclosed in apostrophes
  or required by the IF relational-expression operators. A blank outside
  apostrophes/parentheses terminates the parameter field.
- **Column ranges** (*Chapter 3, Format of statements*):

| Columns | Meaning |
|---|---|
| 1–2 | Identifier (`//`, or `/*` for a delimiter; 1–3 = `//*` for a comment) |
| 3 … | Name field begins in column 3 (or blank if no name) |
| 1–71 | Significant text. **Do not code fields past column 71** (except the comment statement, which may run to column 80). |
| 72 | **Continuation indicator** — see [Continuation](#continuation). |
| 73–80 | Ignored by z/OS; conventionally a sequence number. |

The maximum length of a single (reassembled) JCL statement is **8194
characters**.

> **Ambiguity:** Columns 73–80 are "ignored." This package reads logical text,
> not card images; it treats any content after column 72 on a record as
> non-significant sequence text. Whether the printer re-emits sequence numbers
> is a printer policy decision — by default it should not (canonical output
> omits them).

### Literals

JCL has two literal forms the tokenizer must recognize:

- **Quoted string** (`QuotedString`, seed `TokenString`). An
  apostrophe-delimited value: `'…'`. Used for any value containing special
  characters that would otherwise have syntactic meaning, e.g.
  `ACCT='123+456'` or a programmer name `'O''NEIL'`.
  - **Escape:** a literal apostrophe inside the string is written as **two
    consecutive apostrophes** (`''`). `'O''NEIL'` represents `O'NEIL`.
  - **Contents:** any EBCDIC-text character; blanks, commas, and parentheses
    inside the apostrophes are part of the value, not delimiters.
  - **Spanning records:** an apostrophe-enclosed value may be continued (see
    [Continuation](#continuation)); it is extended to column 71 and resumes in
    column 16 of the next record, *splitting the value mid-character if needed*.
    An apostrophe must **not** be coded in column 71 of a continued record (the
    system would read it as the closing apostrophe).
  - Example: `PARM='/DIR1/DIR2/FILE'`
- **Number** (`Number`, seed `TokenNumber`). An unquoted run of digits used for
  numeric operands and subparameters (return codes, space quantities, generation
  numbers). JCL numbers are plain decimal integers in the grammar; signed forms
  (`+1`, `-2` in a GDG relative generation) appear inside specific parameters and
  combine a `Symbol` sign with a `Number`.
  - Examples: `8` (in `RC > 8`), `1`, `15`

There is no boolean or null literal type; values like `YES`/`NO`,
`DELETE`/`KEEP`, `NEW`/`OLD` are ordinary `Value` tokens interpreted by the
parameter they belong to.

### Keywords and Reserved Words

JCL keywords are **uppercase** and fall into three groups:

- **Operations** (operation field): `JOB`, `EXEC`, `DD`, `PROC`, `PEND`,
  `OUTPUT`, `SET`, `INCLUDE`, `JCLLIB`, `IF`, `ELSE`, `ENDIF`. (The full
  language also defines `CNTL`, `ENDCNTL`, `COMMAND`, `EXPORT`, `XMIT`,
  `SCHEDULE`, and the JES2/JES3 control statements; those are out of scope here
  — see [Out-of-Scope](#out-of-scope--deferred).)
- **Statement keywords** that read as fixed words within a statement: `THEN`
  (closes an IF relational expression), and the IF relational-expression
  keywords `RC`, `ABEND`, `ABENDCC`, `RUN`.
- **Parameter keywords** (left of an `=`): e.g. `PGM`, `PROC`, `PARM`, `COND`,
  `DSN`/`DSNAME`, `DISP`, `MEMBER`, `ORDER`. These are not a closed reserved set
  for the tokenizer — they are `Keyword` tokens recognized by position (a word
  immediately followed by `=`). The exhaustive per-statement list is out of
  scope.

> **Ambiguity:** JCL keywords are conventionally uppercase, and the reference
> codes them in capitals. Whether the tokenizer/parser accepts lowercase
> (`exec`) is unspecified by the reference for the language proper. This package
> should treat operations and keywords as **case-sensitive uppercase** by
> default, matching real JCL and the reference, and revisit only if a fixture
> requires otherwise.

A keyword or subparameter keyword from any JCL/JES2/JES3 statement **must not**
be used as a symbolic-parameter name, a statement name, or a label (*Chapter 3*,
"Use keywords only for parameters or subparameters").

### Symbols and Operators

| Symbol | Role |
|---|---|
| `//` | Statement identifier (columns 1–2). |
| `//*` | Comment-statement identifier (columns 1–3). |
| `/*` | Delimiter-statement identifier, **and** the default end-of-in-stream-data delimiter. |
| `,` | Separates parameters and subparameters. A trailing comma at the break point also signals operand continuation. |
| `=` | Separates a keyword from its value (`DISP=OLD`). |
| `(` `)` | Enclose a subparameter list, a PDS/PDSE member name, or a GDG generation number. |
| `.` | Separates the parts of a qualified data set name (`A.B.C`) and terminates a symbolic parameter before fixed code (see below). |
| `*` | Back-reference (`OUTPUT=*.name`), in-stream data (`DD *`), and special functions (`//label CNTL *`, `RESTART=*`). |
| `&` | Introduces a **symbolic parameter** (`&LIB`). |
| `&&` | Introduces a **temporary data set name** (`&&TEMP`) or, doubled, a literal single ampersand in a value. |
| `+` `-` | Signs inside specific values (GDG relative generation `(+1)`, `(-2)`; account overpunch `+0`). |
| `> < >= <= = ¬ ¬= ¬> ¬< & \|` | IF relational-expression operators (see [IF construct grammar](#grammar-productions)). The alphabetic spellings `GT LT GE LE EQ NE NG NL AND OR NOT` are equivalent. |

### Symbolic parameters

A **symbolic parameter** is a substitution variable in the parameter field
(*Chapter 5, Using system symbols and JCL symbols*):

- **Form:** `&` followed by a name of **1–8** alphanumeric or national
  characters; the first character must be alphabetic or national (`$ # @`).
  Token class `SymbolicParameter`.
  - Example: `&LIB`, `&DAY`, `&SYSUID`
- **Termination:** the name ends at the first character that is **not**
  alphanumeric or national. When the symbol is immediately followed by *fixed
  code* that itself begins with an alphanumeric/national character, a period, or
  a GDG left parenthesis, an explicit **period delimiter** is required and is
  consumed by the substitution:
  - `DSNAME=&DAY.DATA` → with `DAY=MON`, resolves to `DSNAME=MONDATA` (the
    period is the delimiter and disappears).
  - To keep a literal period after a symbol, code **two** periods: `&DEPT..POK`
    with `DEPT=D58` resolves to `D58.POK`.
- **Temporary data set form:** `&&name` names a temporary data set
  (`DSNAME=&&TEMP`); a doubled ampersand elsewhere in a value (`'&&ABC'`) is a
  literal single ampersand, not a symbol (*Table 11/12*).
- **Successive symbols:** two symbols may be coded back-to-back with no comma
  (`PARM=&DECK&CODE`).
- **As a positional parameter:** when a symbol stands for a positional parameter
  followed by more parameters, it is terminated by a period and the substitution
  text supplies any trailing comma: `&POSPARM.DSNAME=ATLAS,DISP=OLD`.

Substitution texts (the *values* of symbols) are assigned by the `SET`, `PROC`,
and `EXEC` statements; see [Semantics](#semantics).

### Continuation

When a statement's fields would exceed column 71, the statement continues onto
one or more following records (*Chapter 3, Continuing JCL statements*). The
rules differ by which field is continued. **The comment statement, the
delimiter statement, the null statement, and the JCL command statement cannot be
continued.**

**Continuing the parameter (operand) field:**

1. Interrupt the field after a **complete parameter or subparameter, including
   the comma that follows it**, at or before column 71.
2. Code `//` in columns 1–2 of the next record.
3. Code a **blank in column 3** (column 3 must be blank or `*`; otherwise the
   record is read as a new statement and the job fails with a "no continuation
   found" error).
4. Resume the interrupted operand beginning in any column from **4 through 16**.

A nonblank character in column 72 is **optional** for operand continuation — the
trailing comma is what signals it.

**Continuing an apostrophe-enclosed value:**

1. Extend the value to column 71 (do **not** place an apostrophe in column 71).
2. Code `//` in columns 1–2 of the next record.
3. Resume the value in **column 16**, even if this splits the value
   mid-character. Trailing blanks/commas inside the apostrophes are part of the
   value, not a continuation signal.

**Continuing the comments field:**

1. Interrupt the comment at any convenient point up to and including column 71.
2. Code a **nonblank character in column 72** — this is **required** for comment
   continuation (conventionally `X`).
3. Code `//` in columns 1–2 and a blank in column 3 of the next record.
4. Resume the comment in any column after column 3.

If a statement continues both its parameter field and a trailing comment on the
same record, the system ignores the comment-continuation indication.

> **Ambiguity:** Column 72 is a *required* indicator for comment continuation
> but *optional* for operand continuation (where the trailing comma is
> authoritative). The tokenizer must reassemble continued records into one
> logical statement before the parser sees it, applying the resume-column rules
> (4–16 for operands, 16 for apostrophe values, >3 for comments). The reference
> specifies the indicator column and resume columns precisely; it does not
> require the printer to reproduce any particular continuation layout — the
> printer chooses a canonical break (see [Semantics](#semantics)).

## Structure (Grammar)

This section describes the parser's view: how the token stream forms statements
and how statements group into a job. All productions are EBNF over the token
classes in [Lexical Elements](#lexical-elements-tokens). `Newline` is the record
boundary after continuation reassembly; `Blank` is the inter-field delimiter.

### Top-Level Structure

A complete JCL input is a **job**: a `JOB` statement followed by the statements
that make up its steps, optionally ended by a null statement. (Separately stored
**cataloged procedures** and **INCLUDE groups** are parsed standalone — a body
of statements with no leading `JOB` — and resolved into a job later; see
[Semantics](#semantics).)

```ebnf
File          = Job | ProcBody ;
Job           = JobStatement { BodyStatement } [ NullStatement ] ;
ProcBody      = { BodyStatement } ;        (* standalone PROC / INCLUDE member *)

BodyStatement = ExecStatement
              | DDStatement
              | ProcStatement
              | PendStatement
              | OutputStatement
              | SetStatement
              | IncludeStatement
              | JcllibStatement
              | IfConstruct
              | CommentStatement
              | NullStatement
              | DelimiterStatement ;
```

Document-level constraints: a job begins with exactly one `JOB` statement; a
comment statement (`//*`) or null statement (`//`) may appear anywhere; an
in-stream `PROC`/`PEND` pair and an `IF`/`ENDIF` construct must be balanced (see
[Ordering and Optionality](#ordering-and-optionality)).

### Grammar Productions

Every statement shares the same skeleton — identifier, optional name, operation,
optional parameter field, optional comments — captured once and then specialized
per operation. Field forms are taken from *Table 8. JCL Statement Fields*.

```ebnf
(* shared skeleton *)
Name           = NameToken ;                       (* 1–8 alnum/national, begins col 3 *)
Comments       = { CommentText } ;                 (* free text, ignored *)

(* JOB: name is required *)
JobStatement   = "//" Name Blank "JOB" [ Blank ParameterField ]
                 [ Blank Comments ] Newline ;

(* EXEC: name (stepname) optional; operands required *)
ExecStatement  = "//" [ Name ] Blank "EXEC" Blank ExecOperands
                 [ Blank Comments ] Newline ;
ExecOperands   = ( "PGM" "=" Value | "PROC" "=" Value | Value )   (* PGM= or PROC= or positional procname *)
                 { "," Parameter } ;

(* DD: name (ddname) optional; operands optional; * / DATA introduce in-stream data *)
DDStatement    = "//" [ Name ] Blank "DD" [ Blank ParameterField ]
                 [ Blank Comments ] Newline
               | InStreamDD ;
InStreamDD     = "//" [ Name ] Blank "DD" Blank ( "*" | "DATA" )
                 [ "," ParameterField ] Newline
                 InStreamData
                 DelimiterStatement ;                (* /* or DLM-defined delimiter *)

(* PROC / PEND: in-stream procedure brackets; cataloged PROC may also stand alone *)
ProcStatement  = "//" [ Name ] Blank "PROC" [ Blank ParameterField ]
                 [ Blank Comments ] Newline ;
PendStatement  = "//" [ Name ] Blank "PEND" [ Blank Comments ] Newline ;

(* OUTPUT, SET, INCLUDE, JCLLIB: keyword-parameter statements *)
OutputStatement  = "//" Name Blank "OUTPUT" Blank ParameterField
                   [ Blank Comments ] Newline ;
SetStatement     = "//" [ Name ] Blank "SET" Blank SymbolAssignment
                   { "," SymbolAssignment } [ Blank Comments ] Newline ;
SymbolAssignment = Name "=" Value ;                  (* &-less name; defines a JCL symbol *)
IncludeStatement = "//" [ Name ] Blank "INCLUDE" Blank "MEMBER" "=" Value
                   [ Blank Comments ] Newline ;
JcllibStatement  = "//" [ Name ] Blank "JCLLIB" Blank ParameterField
                   [ Blank Comments ] Newline ;      (* e.g. ORDER=(lib1,lib2) *)

(* IF / THEN / ELSE / ENDIF construct *)
IfConstruct    = IfStatement { BodyStatement }
                 [ ElseStatement { BodyStatement } ]
                 EndifStatement ;
IfStatement    = "//" [ Name ] Blank "IF" Blank [ "(" ] RelExpr [ ")" ]
                 Blank "THEN" [ Blank Comments ] Newline ;
ElseStatement  = "//" [ Name ] Blank "ELSE" [ Blank Comments ] Newline ;
EndifStatement = "//" [ Name ] Blank "ENDIF" [ Blank Comments ] Newline ;

(* trivial statements *)
CommentStatement   = "//*" CommentText Newline ;
NullStatement      = "//" Newline ;
DelimiterStatement = "/*" [ Blank Comments ] Newline ;
```

**Parameter field** (the general grammar — the per-statement keyword catalog is
out of scope; *Chapter 4, Parameter field*):

```ebnf
ParameterField   = Parameter { "," Parameter } ;
Parameter        = PositionalParameter | KeywordParameter ;
PositionalParameter = Value | SubparameterList ;
KeywordParameter = Keyword "=" ( Value | SubparameterList ) ;

SubparameterList = "(" [ Subparameter ] { "," [ Subparameter ] } ")" ;
Subparameter     = PositionalParameter | KeywordParameter ;   (* may be empty: null subparameter *)

Value            = Scalar { "." Scalar }              (* qualified name: A.B.C *)
                 | QuotedString
                 | SymbolicParameter ;
Scalar           = Word | Number ;                    (* alnum/national run, or a number *)
```

- A `Keyword "=" SubparameterList` covers `DISP=(NEW,KEEP,DELETE)`,
  `SPACE=(TRK,(10,5))`, etc. A single-item list may omit the parentheses
  (`DISP=OLD`).
- A `SubparameterList` element may be **null** (omitted) — two adjacent commas,
  or a leading/trailing comma — representing an omitted positional subparameter:
  `DISP=(,KEEP)`.
- A `QuotedString` value may itself contain commas/blanks/parentheses (they are
  literal inside apostrophes).

**IF relational-expression** (*Chapter 18*):

```ebnf
RelExpr     = RelTerm { LogicalOp RelTerm } ;
RelTerm     = [ NotOp ] ( "(" RelExpr ")" | Comparison ) ;
Comparison  = CondOperand [ CompareOp Number ] ;
CondOperand = "RC" | "ABEND" | "ABENDCC" | "RUN"
            | StepName [ "." ProcStepName ] [ "." "RC" | "." "ABEND" | "." "RUN" ] ;
CompareOp   = ">" | "<" | ">=" | "<=" | "=" | "¬=" | "¬>" | "¬<"
            | "GT" | "LT" | "GE" | "LE" | "EQ" | "NE" | "NG" | "NL" ;
LogicalOp   = "&" | "|" | "AND" | "OR" ;
NotOp       = "¬" | "NOT" ;
```

- Operator priority is NOT (highest), then comparison, then logical (lowest).
- Special-character operators (`>`, `¬=`) need no surrounding blanks, but the
  logical `&` and `|` **must** be preceded and followed by a blank so `&` is not
  read as a symbolic parameter.

### Ordering and Optionality

- **Positional before keyword.** Within a parameter field, all positional
  parameters precede all keyword parameters. An omitted positional parameter
  that is followed by another positional parameter is marked by a placeholder
  comma; trailing omitted positionals (or when only keyword parameters follow,
  or when all positionals are omitted) take **no** placeholder comma. A keyword
  parameter's absence is **never** marked by a comma.
- **Name field.** Required on `JOB` and `OUTPUT`; optional on `EXEC`, `DD`,
  `PROC` (required for in-stream `PROC`), `PEND`, `SET`, `INCLUDE`, `JCLLIB`,
  `IF`/`ELSE`/`ENDIF`. When omitted, column 3 must be blank. A name is 1–8
  alphanumeric/national characters, first character alphabetic/national.
- **PROC/PEND pairing.** An in-stream procedure is bracketed by a named `PROC`
  statement and a `PEND` statement; statements between them form the procedure
  body. A cataloged procedure (separate member) has an optional `PROC` and no
  `PEND`.
- **IF/ENDIF nesting.** Each `IF` has a matching `ENDIF`; an optional `ELSE`
  sits between the THEN-body and the `ENDIF`. Constructs nest up to 15 levels;
  a THEN- or ELSE-body may contain another `IfConstruct`.
- **In-stream data.** A `DD *` or `DD DATA` statement is followed by data
  records up to a delimiter statement (`/*` by default, or the value set by a
  `DLM=` parameter). `DD DATA` lets the data contain `//`; `DD *` ends at the
  next `//` or `/*`.
- **JOB first.** The `JOB` statement is the first statement of a job; `JCLLIB`,
  if present, comes early (after `JOB`, before the first `EXEC` that needs it);
  `SET` must precede the first use of the symbols it defines.

## Semantics

Meaning and interpretation rules that shape the AST and the printer's output —
beyond whether a statement merely parses.

- **Symbolic-parameter substitution.** A `&name` is replaced by its substitution
  text before the affected statement is interpreted. Substitution texts are
  assigned, in increasing precedence, by the `PROC` statement (defaults), a
  `SET` statement (`SET name=text`, last assignment wins on a single SET), and
  the invoking `EXEC` statement (overrides PROC defaults; first assignment wins
  on a single EXEC). A symbol may be **nullified** (assigned empty), which
  removes it and any delimiting period from the text. The same symbol may take
  different texts at different points in a job. *The package parses symbols as
  AST nodes; whether and when it performs substitution (`Expand`) is a separate
  resolution step, not part of `Parse`.*
- **`&&name` temporary data sets.** `&&name` in a `DSNAME`/`DSN` denotes a
  temporary data set scoped to the job; the same `&&name` in a later step refers
  to the same temporary data set. A doubled ampersand inside an apostrophe value
  is a literal `&`.
- **PROC / INCLUDE expansion.** An `EXEC PROC=name` (or `EXEC name`) invokes a
  procedure; its statements are spliced into the job with the invoker's symbol
  overrides applied. An `INCLUDE MEMBER=name` is replaced in place by the
  statements of the named INCLUDE group; included statements must be complete
  (an included statement cannot continue the statement preceding the `INCLUDE`).
  Both are resolved against a procedure/INCLUDE library (`Expand` /
  `ParseProc`); the effective job is the result of all substitutions and
  splices.
- **Comments and null statements are non-semantic.** A `//*` comment, a trailing
  comments field, and a `//` null statement carry no execution meaning. The AST
  records them (so the printer can round-trip them) but they affect nothing
  else.
- **Continuation is purely lexical.** A statement split across records is **one**
  logical statement; the AST has no notion of which record a token came from.
  The printer re-emits a canonical continuation layout and need not reproduce the
  original column breaks — so round-trip equality is checked on the AST
  (ignoring `Pos`), not byte-for-byte.
- **IF evaluation.** A relational expression evaluates to true/false at execution
  time from return codes / abend status; this is runtime semantics the package
  does not execute. The parser only records the expression tree. A `stepname` in
  an expression that did not run makes the expression false (runtime rule, noted
  for completeness).
- **Equivalence.** `GT` and `>` (etc.) are the same operator; the AST should
  normalize them to one representation so `(RC GT 4)` and `(RC > 4)` compare
  equal. A single-item subparameter list with or without parentheses
  (`DISP=OLD` vs `DISP=(OLD)`) denotes the same value.

## Examples

Three complete, valid jobs. Each is coded within columns 1–71 and parses under
the grammar above; they are the seed fixtures for tokenizer / parser / printer
round-trips.

### Minimal Valid File

A one-step job that runs a program and does nothing else (`IEFBR14` is the
classic no-op utility):

```jcl
//MINIMAL  JOB  (ACCT),'JANE DOE'
//STEP1    EXEC PGM=IEFBR14
```

### Typical File

A realistic job: accounting and class on `JOB`, a program step, a cataloged
output DD with a disposition subparameter list, a SYSOUT DD, and in-stream data
ended by a delimiter:

```jcl
//PAYROLL  JOB  (D58),'PAYROLL RUN',CLASS=A,MSGCLASS=X,MSGLEVEL=(1,1)
//*
//* MONTHLY PAYROLL POSTING
//*
//POST     EXEC PGM=PAYPOST
//OUTFILE  DD   DSN=PROD.PAYROLL.MASTER,DISP=(MOD,KEEP,KEEP),
//             UNIT=SYSDA,SPACE=(CYL,(10,5))
//REPORT   DD   SYSOUT=A
//SYSIN    DD   *
POST PERIOD=202406
BALANCE ACCOUNT=ALL
/*
//
```

This shows: a continued operand (`OUTFILE` breaks after the comma following
`DISP=(MOD,KEEP,KEEP),` and resumes in column 16), a subparameter list, comment
statements, in-stream data after `DD *`, the `/*` delimiter, and a closing null
statement.

### Complex File

A job that exercises symbolic parameters, an in-stream PROC/PEND, an
IF/THEN/ELSE/ENDIF construct, a quoted string with an embedded apostrophe, a
continued comment (column-72 `X`), and the `&&` temporary data set form:

```jcl
//BUILD    JOB  (ENG),'O''NEIL',CLASS=B,NOTIFY=&SYSUID
//MYPROC   PROC MBR=DEFAULT,OPT=NOOPT
//RUN      EXEC PGM=&MBR,PARM='OPTIONS(&OPT)'
//WORK     DD   DSN=&&TEMP,UNIT=SYSDA,SPACE=(TRK,(5,5)),
//              DISP=(NEW,PASS)
//         PEND
//*
//STEP1    EXEC MYPROC,MBR=COMPILE,OPT=OPT2
//CHECK    IF (RC > 4 & RC < 12) THEN                                  X
//*            RERUN WITH DIAGNOSTICS WHEN RETURN CODE IS MARGINAL
//RERUN    EXEC PGM=DIAG,PARM='/VERBOSE/DIR1/DIR2/DIR3/DIR4/DIR5/DIR6/D
//             IR7/REPORT'
//         ELSE
//DONE     EXEC PGM=IEFBR14
//         ENDIF
//
```

This exercises: a quoted programmer name with a doubled apostrophe (`'O''NEIL'`),
the `&SYSUID` system symbol, an in-stream procedure (`MYPROC`/`PEND`) with
symbolic defaults `&MBR`/`&OPT` overridden on the invoking `EXEC MYPROC`, a
`&&TEMP` temporary data set, a continued operand on `WORK`, an IF/THEN/ELSE/ENDIF
construct with a compound relational expression, a comment statement, and a
continued apostrophe-enclosed `PARM` value (`RERUN` breaks at column 71 and
resumes in column 16).

## Appendix

### Character Encoding

JCL is historically an **EBCDIC** language; the reference defines its character
sets over EBCDIC code points (`@` = X'7C', `$` = X'5B', `#` = X'7B', EBCDIC text
X'40'–X'FE'). This package reads source from an `io.Reader` as bytes/runes
(normally ASCII/UTF-8) and models the *glyphs*, not the code points. Source is
conventionally **uppercase**; values inside apostrophes may contain any
printable character. There is no byte-order mark and no in-band encoding
declaration. Records are at most 80 bytes; significant text is columns 1–71
(through 80 for the comment statement); columns 73–80 are ignored sequence text.

### Out-of-Scope / Deferred

The following are part of the JCL language but **not specified here**; they are
left to later stories or are outside the package's goal:

- The **exhaustive keyword-parameter catalog** for each statement (every JOB,
  EXEC, DD, OUTPUT, etc. keyword and its subparameters). This document specifies
  only the *general* parameter grammar.
- **JES2** (`/*…`) and **JES3** (`//*…`) control statements, and the `COMMAND`,
  `CNTL`/`ENDCNTL`, `EXPORT`, `XMIT`, and `SCHEDULE` statements.
- Full **procedure/INCLUDE resolution** semantics (nested-procedure symbol
  scoping, override/merge rules) beyond the summary in [Semantics](#semantics);
  the resolving API is `ParseProc` / `Expand`.
- Runtime **evaluation** of IF relational expressions and COND processing (the
  package parses, it does not execute).
- The complete **reserved-word / system-symbol** registries (e.g. the full set
  of static/dynamic system symbols), which live in *z/OS MVS Initialization and
  Tuning Reference*.

### Implementation Notes

- The tokenizer must **reassemble continuation records** into one logical
  statement before classifying the parameter/comments fields, applying the
  resume-column rules (operands 4–16, apostrophe values 16, comments >3) and the
  column-72 indicator (required for comments, optional for operands).
- `Name`/`Operation`/`Keyword`/`Value` are lexically one class (`Identifier`);
  the parser separates them by position. An implementation may emit the refined
  classes from the tokenizer instead — see the ambiguity note in [Lexical
  Elements](#lexical-elements-tokens).
- The printer emits a **canonical** layout (its own continuation breaks, no
  sequence numbers); round-trip tests compare ASTs ignoring `Pos`.

### Related Standards

- **IBM z/OS MVS JCL Reference**, SA23-1385 (z/OS V2R4) — the governing
  reference. <https://www.ibm.com/docs/en/SSLTBW_2.4.0/pdf/ieab600_v2r4.pdf>
- **IBM z/OS MVS JCL User's Guide**, SA23-1386 — task-oriented companion.
- **IBM z/OS MVS Initialization and Tuning Reference** — the system-symbol
  registry referenced by [Symbolic parameters](#symbolic-parameters).
