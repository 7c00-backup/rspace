// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main // TODO: package lex

import (
	"fmt"
	"go/build"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
	"unicode"

	"code.google.com/p/rsc/c2go/liblink"
)

// A Token represents an input item. It is a simple wrapping of rune, as
// returned by text/scanner.Scanner, plus a couple of extra values.
type Token rune

const (
	// Asm defines some two-character lexemes. We make up
	// a rune/Token value for them - ugly but simple.
	LSH Token = -1000 - iota // << Left shift.
	RSH                      // >> Logical right shift.
	ARR                      // -> Used on ARM for shift type 3, arithmetic right shift.
	ROT                      // -> Used on ARM for shift type 4, rotate right.
)

func (t Token) String() string {
	switch t {
	case scanner.EOF:
		return "EOF"
	case scanner.Ident:
		return "identifier"
	case scanner.Int:
		return "integer constant"
	case scanner.Float:
		return "float constant"
	case scanner.Char:
		return "rune constant"
	case scanner.String:
		return "string constant"
	case scanner.RawString:
		return "raw string constant"
	case scanner.Comment:
		return "comment"
	default:
		return fmt.Sprintf("%q", rune(t))
	}
}

var linkCtxt *liblink.Link
var histline int = 1

func NewLexer(name string, ctxt *liblink.Link, dFlag, iFlag multiFlag) TokenReader {
	input := NewInput(name, dFlag, iFlag)
	fd, err := os.Open(name)
	if err != nil {
		log.Fatalf("asm: %s\n", err)
	}
	linkCtxt = ctxt
	input.Push(NewTokenizer(name, fd))
	return input
}

// A TokenReader is like a reader, but returns lex tokens of type Token. It also can tell you what
// the text of the most recently returned token is, and where it was found.
// The underlying scanner elides all spaces except newline, so the input looks like a  stream of
// Tokens; original spacing is lost but we don't need it.
type TokenReader interface {
	// Next returns the next token.
	Next() Token
	// The following methods all refer to the most recent token returned by Next.
	// Text returns the original string representation of the token.
	Text() string
	// FileName reports the source file name of the token.
	FileName() string
	// Line reports the source line number of the token.
	Line() int
	// SetPos sets the file and line number.
	SetPos(line int, file string)
}

// A LexToken is a token and its string value.
// A macro is stored as a sequence of LexTokens with spaces stripped.
type LexToken struct {
	Token
	text string
}

func (l LexToken) String() string {
	return l.text
}

// A Macro represents the definition of a #defined macro.
type Macro struct {
	name   string
	args   []string
	tokens []LexToken
}

// tokenize turns a string into a list of LexTokens; used to parse the -D flag.
func tokenize(str string) []LexToken {
	t := NewTokenizer("command line", strings.NewReader(str))
	var tokens []LexToken
	for {
		tok := t.Next()
		if tok == scanner.EOF {
			break
		}
		tokens = append(tokens, LexToken{Token: tok, text: t.Text()})
	}
	return tokens
}

// The rest of this file is implementations of TokenReader.

// A Tokenizer is a simple wrapping of text/scanner.Scanner, configured
// for our purposes and made a TokenReader. It forms the lowest level,
// turning text from readers into tokens.
type Tokenizer struct {
	tok      Token
	s        *scanner.Scanner
	line     int
	fileName string
}

func NewTokenizer(name string, r io.Reader) *Tokenizer {
	var s scanner.Scanner
	s.Init(r)
	// Newline is like a semicolon; other space characters are fine.
	s.Whitespace = 1<<'\t' | 1<<'\r' | 1<<' '
	// Don't skip comments: we need to count newlines.
	s.Mode = scanner.ScanChars |
		scanner.ScanFloats |
		scanner.ScanIdents |
		scanner.ScanInts |
		scanner.ScanStrings |
		scanner.ScanComments
	s.Position.Filename = name
	s.IsIdentRune = isIdentRune
	liblink.Linklinehist(linkCtxt, histline, name, 0)
	return &Tokenizer{
		s:        &s,
		line:     1,
		fileName: name,
	}
}

// We want center dot (·) and division slash (∕) to work as identifier characters.
func isIdentRune(ch rune, i int) bool {
	if unicode.IsLetter(ch) {
		return true
	}
	switch ch {
	case '_': // Underscore; traditional.
		return true
	case '\u00B7': // Represents the period in runtime.exit.
		return true
	case '\u2215': // Represents the slash in runtime/debug.setGCPercent
		return true
	}
	// Digits are OK only after the first character.
	return i > 0 && unicode.IsDigit(ch)
}

func (t *Tokenizer) Text() string {
	switch t.tok {
	case LSH:
		return "<<"
	case RSH:
		return ">>"
	case ARR:
		return "->"
	case ROT:
		return "@>"
	}
	return t.s.TokenText()
}

func (t *Tokenizer) FileName() string {
	return t.fileName
}

func (t *Tokenizer) Line() int {
	return t.line
}

func (t *Tokenizer) SetPos(line int, file string) {
	t.line = line
	t.fileName = file
}

func (t *Tokenizer) Next() Token {
	s := t.s
	for {
		t.tok = Token(s.Scan())
		if t.tok != scanner.Comment {
			break
		}
		t.line += strings.Count(s.TokenText(), "\n")
		// TODO: If we ever have //go: comments in assembly, will need to keep them here.
		// For now, just discard all comments.
	}
	switch t.tok {
	case '\n':
		histline++
		t.line++
	case '-':
		if s.Peek() == '>' {
			s.Next()
			t.tok = ARR
			return ARR
		}
	case '@':
		if s.Peek() == '>' {
			s.Next()
			t.tok = ROT
			return ROT
		}
	case '<':
		if s.Peek() == '<' {
			s.Next()
			t.tok = LSH
			return LSH
		}
	case '>':
		if s.Peek() == '>' {
			s.Next()
			t.tok = RSH
			return RSH
		}
	}
	return t.tok
}

// A Stack is a stack of TokenReaders. As the top TokenReader hits EOF,
// it resumes reading the next one down.
type Stack struct {
	tr []TokenReader
}

// Push adds tr to the top of the input stack. (Popping happens automatically.)
func (s *Stack) Push(tr TokenReader) {
	s.tr = append(s.tr, tr)
}

func (s *Stack) Next() Token {
	tok := s.tr[len(s.tr)-1].Next()
	for tok == scanner.EOF && len(s.tr) > 1 {
		// Pop the topmost item from the stack and resume with the next one down.
		// TODO: close file descriptor.
		liblink.Linklinehist(linkCtxt, histline, "XXXXXXX", 0) // TODO: what to do here?
		s.tr = s.tr[:len(s.tr)-1]
		tok = s.Next()
	}
	return tok
}

func (s *Stack) Text() string {
	return s.tr[len(s.tr)-1].Text()
}

func (s *Stack) FileName() string {
	return s.tr[len(s.tr)-1].FileName()
}

func (s *Stack) Line() int {
	return s.tr[len(s.tr)-1].Line()
}

func (s *Stack) SetPos(line int, file string) {
	s.tr[len(s.tr)-1].SetPos(line, file)
}

// A Slice reads from a slice of LexTokens.
type Slice struct {
	tokens   []LexToken
	fileName string
	line     int
	pos      int
}

func NewSlice(fileName string, line int, tokens []LexToken) *Slice {
	return &Slice{
		tokens:   tokens,
		fileName: fileName,
		line:     line,
		pos:      -1, // Next will advance to zero.
	}
}

func (s *Slice) Next() Token {
	s.pos++
	if s.pos >= len(s.tokens) {
		return scanner.EOF
	}
	return s.tokens[s.pos].Token
}

func (s *Slice) Text() string {
	return s.tokens[s.pos].text
}

func (s *Slice) FileName() string {
	return s.fileName
}

func (s *Slice) Line() int {
	return s.line
}

func (s *Slice) SetPos(line int, file string) {
	// Cannot happen because we only have slices of already-scanned
	// text, but be prepared.
	s.line = line
	s.fileName = file
}

// Input is the main input: a stack of readers and some macro definitions.
// It also handles #include processing (by pushing onto the input stack)
// and parses and instantiates macro definitions.
type Input struct {
	Stack
	includes        []string
	beginningOfLine bool
	ifdefStack      []bool
	macros          map[string]*Macro
}

func NewInput(name string, defines, includes multiFlag) *Input {
	return &Input{
		// include directories: look in source dir, then -I directories.
		includes:        append([]string{filepath.Dir(name)}, includes...),
		beginningOfLine: true,
		macros:          predefine(defines),
	}
}

// predefine installs the macros set by the -D flag on the command line.
func predefine(defines multiFlag) map[string]*Macro {
	macros := make(map[string]*Macro)
	for _, name := range defines {
		value := "1"
		i := strings.IndexRune(name, '=')
		if i > 0 {
			name, value = name[:i], name[i+1:]
		}
		tokens := tokenize(name)
		if len(tokens) != 1 || tokens[0].Token != scanner.Ident {
			usage()
		}
		macros[name] = &Macro{
			name:   name,
			args:   nil,
			tokens: tokenize(value),
		}
	}
	return macros
}

func (in *Input) Error(args ...interface{}) {
	fmt.Fprintf(os.Stderr, "asm: %s:%d: %s", in.FileName(), in.Line(), fmt.Sprintln(args...))
	os.Exit(1)
}

// expect is like Error but adds "got XXX" where XXX is a quoted representation of the most recent token.
func (in *Input) expectText(args ...interface{}) {
	in.Error(append(args, "; got", strconv.Quote(in.Text()))...)
}

// including reports whether the input is enabled by an ifdef, or is at the top level.
func (in *Input) including() bool {
	return len(in.ifdefStack) == 0 || in.ifdefStack[len(in.ifdefStack)-1]
}

func (in *Input) expectNewline(directive string) {
	tok := in.Stack.Next()
	if tok != '\n' {
		in.expectText("expected newline after", directive)
	}
}

func (in *Input) Next() Token {
	for {
		tok := in.Stack.Next()
		switch tok {
		case '#':
			if !in.beginningOfLine {
				in.Error("'#' must be first item on line")
			}
			in.beginningOfLine = in.hash()
		case scanner.Ident:
			// Is it a macro name?
			name := in.Stack.Text()
			macro := in.macros[name]
			if macro != nil {
				in.invokeMacro(macro)
				continue
			}
			fallthrough
		default:
			in.beginningOfLine = tok == '\n'
			if in.including() {
				return tok
			}
		}
	}
	in.Error("recursive macro invocation")
	return 0
}

// hash processes a # preprocessor directive. It returns true iff it completes.
func (in *Input) hash() bool {
	// We have a #, it must be followed by a known word (define, include, etc.).
	tok := in.Stack.Next()
	if tok != scanner.Ident {
		in.expectText("expected identifier after '#'")
	}
	if !in.including() {
		// Can only start including again if we are at #else or #endif.
		// We let #line through because it might affect errors.
		switch in.Text() {
		case "else", "endif", "line":
			// Press on.
		default:
			return false
		}
	}
	switch in.Text() {
	case "define":
		in.define()
	case "else":
		in.else_()
	case "endif":
		in.endif()
	case "ifdef":
		in.ifdef(true)
	case "ifndef":
		in.ifdef(false)
	case "include":
		in.include()
	case "line":
		in.line()
	case "undef":
		in.undef()
	default:
		in.Error("unexpected identifier after '#':", in.Text())
	}
	return true
}

// macroName returns the name for the macro being referenced.
func (in *Input) macroName() string {
	// We use the Stacks' input method; no macro processing at this stage.
	tok := in.Stack.Next()
	if tok != scanner.Ident {
		in.expectText("expected identifier after # directive")
	}
	// Name is alphanumeric by definition.
	return in.Text()
}

// #define processing.
func (in *Input) define() {
	name := in.macroName()
	args, tokens := in.macroDefinition(name)
	in.defineMacro(name, args, tokens)
}

// defineMacro stores the macro definition in the Input.
func (in *Input) defineMacro(name string, args []string, tokens []LexToken) {
	if in.macros[name] != nil {
		in.Error("redefinition of macro:", name)
	}
	in.macros[name] = &Macro{
		name:   name,
		args:   args,
		tokens: tokens,
	}
}

// macroDefinition returns the list of formals and the tokens of the definition.
// The argument list is nil for no parens on the definition; otherwise a list of
// formal argument names.
func (in *Input) macroDefinition(name string) ([]string, []LexToken) {
	tok := in.Stack.Next()
	if tok == '\n' || tok == scanner.EOF {
		in.Error("no definition for macro:", name)
	}
	var args []string
	if tok == '(' {
		// Macro has arguments. Scan list of formals.
		acceptArg := true
	Loop:
		for {
			tok = in.Stack.Next()
			switch tok {
			case ')':
				tok = in.Stack.Next() // First token of macro definition.
				break Loop
			case ',':
				if acceptArg {
					in.Error("bad syntax in definition for macro:", name)
				}
				acceptArg = true
			case scanner.Ident:
				if !acceptArg {
					in.Error("bad syntax in definition for macro:", name)
				}
				arg := in.Stack.Text()
				if i := lookup(args, arg); i >= 0 {
					in.Error("duplicate argument", arg, "in definition for macro:", name)
				}
				args = append(args, arg)
				acceptArg = false
			default:
				in.Error("bad definition for macro:", name)
			}
		}
	}
	var tokens []LexToken
	// Scan to newline. Backslashes escape newlines.
	for tok != '\n' {
		if tok == '\\' {
			tok = in.Stack.Next()
			if tok != '\n' && tok != '\\' {
				in.Error(`can only escape \ or \n in definition for macro:`, name)
			}
			if tok == '\n' { // backslash-newline is discarded
				tok = in.Stack.Next()
				continue
			}
		}
		tokens = append(tokens, LexToken{Token(tok), in.Text()})
		tok = in.Stack.Next()
	}
	return args, tokens
}

func lookup(args []string, arg string) int {
	for i, a := range args {
		if a == arg {
			return i
		}
	}
	return -1
}

// invokeMacro pushes onto the input Stack a Slice that holds the macro definition with the actual
// parameters substituted for the formals.
func (in *Input) invokeMacro(macro *Macro) {
	actuals := in.argsFor(macro)
	var tokens []LexToken
	for _, tok := range macro.tokens {
		if tok.Token != scanner.Ident {
			tokens = append(tokens, tok)
			continue
		}
		substitution := actuals[tok.text]
		if substitution == nil {
			tokens = append(tokens, tok)
			continue
		}
		tokens = append(tokens, substitution...)
	}
	in.Push(NewSlice(in.FileName(), in.Line(), tokens))
}

// argsFor returns a map from formal name to actual value for this macro invocation.
func (in *Input) argsFor(macro *Macro) map[string][]LexToken {
	if macro.args == nil {
		return nil
	}
	tok := in.Stack.Next()
	if tok != '(' {
		in.Error("missing arguments for invocation of macro:", macro.name)
	}
	var tokens []LexToken
	args := make(map[string][]LexToken)
	argNum := 0
	for {
		tok = in.Stack.Next()
		switch tok {
		case scanner.EOF, '\n':
			in.Error("unterminated arg list invoking macro:", macro.name)
		case ',', ')':
			if argNum >= len(macro.args) {
				in.Error("too many arguments for macro:", macro.name)
			}
			args[macro.args[argNum]] = tokens
			tokens = nil
			argNum++
			if tok == ')' {
				if argNum != len(macro.args) {
					in.Error("too few arguments for macro:", macro.name)
				}
				return args
			}
		default:
			tokens = append(tokens, LexToken{tok, in.Stack.Text()})
		}
	}
}

// #ifdef and #ifndef processing.
func (in *Input) ifdef(truth bool) {
	name := in.macroName()
	in.expectNewline("#if[n]def")
	if _, defined := in.macros[name]; !defined {
		truth = !truth
	}
	in.ifdefStack = append(in.ifdefStack, truth)
}

// #else processing
func (in *Input) else_() {
	in.expectNewline("#else")
	if len(in.ifdefStack) == 0 {
		in.Error("unmatched #else")
	}
	in.ifdefStack[len(in.ifdefStack)-1] = !in.ifdefStack[len(in.ifdefStack)-1]
}

// #endif processing.
func (in *Input) endif() {
	in.expectNewline("#endif")
	if len(in.ifdefStack) == 0 {
		in.Error("unmatched #endif")
	}
	in.ifdefStack = in.ifdefStack[:len(in.ifdefStack)-1]
}

// #include processing.
func (in *Input) include() {
	// Find and parse string.
	tok := in.Stack.Next()
	if tok != scanner.String {
		in.expectText("expected string after #include")
	}
	name, err := strconv.Unquote(in.Text())
	if err != nil {
		in.Error("unquoting include file name: ", err)
	}
	in.expectNewline("#include")
	// Replace GOOS and GOARCH as required.
	name = strings.Replace(name, "_GOOS", "_"+build.Default.GOOS, -1)
	name = strings.Replace(name, "_GOARCH", "_"+build.Default.GOARCH, -1)
	// Push tokenizer for file onto stack.
	fd, err := os.Open(name)
	if err != nil {
		for _, dir := range in.includes {
			fd, err = os.Open(filepath.Join(dir, name))
			if err == nil {
				break
			}
		}
		if err != nil {
			in.Error("#include:", err)
		}
	}
	println("#INCLUDE", name, histline)
	liblink.Linklinehist(linkCtxt, histline, name, 0)
	in.Push(NewTokenizer(name, fd))
}

// #line processing.
func (in *Input) line() {
	// Only need to handle Plan 9 format: #line 337 "filename"
	tok := in.Stack.Next()
	if tok != scanner.Int {
		in.expectText("expected line number after #line")
	}
	line, err := strconv.Atoi(in.Stack.Text())
	if err != nil {
		in.Error("error parsing #line (cannot happen):", err)
	}
	tok = in.Stack.Next()
	if tok != scanner.String {
		in.expectText("expected file name in #line")
	}
	file, err := strconv.Unquote(in.Stack.Text())
	if err != nil {
		in.Error("unquoting #line file name: ", err)
	}
	println("#LINE", histline, line, file)
	liblink.Linklinehist(linkCtxt, histline, file, line)
	in.Stack.SetPos(line, file)
}

// #undef processing
func (in *Input) undef() {
	name := in.macroName()
	if in.macros[name] == nil {
		in.Error("#undef for undefined macro:", name)
	}
	// Newline must be next.
	tok := in.Stack.Next()
	if tok != '\n' {
		in.Error("syntax error in #undef for macro:", name)
	}
	delete(in.macros, name)
}

func (in *Input) Push(r TokenReader) {
	if len(in.tr) > 100 {
		in.Error("input recursion")
	}
	in.Stack.Push(r)
}
