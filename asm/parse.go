// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"text/scanner"

	"code.google.com/p/rsc/c2go/liblink"
	"code.google.com/p/rsc/c2go/liblink/amd64" // TODO: configure the architecture
)

var (
	instructions     = make(map[string]int)
	registers        = make(map[string]int)
	pseudos          = make(map[string]int) // TEXT, DATA etc.
	unaryDestination = make(map[int]bool)   // Instruction takes one operand and result is a destination.
)

const (
	rFP = -(iota + 1)
	rSB
	rSP
	rPC
)

func init() {
	// Create maps for easy lookup of instruction names etc.
	// TODO: Should this be done in liblink for us?
	for i, s := range amd64.Regstr {
		registers[s] = i
	}
	// Pseudo-registers.
	registers["SB"] = rSB
	registers["FP"] = rFP
	registers["SP"] = rSP // TODO: is this amd64-only?
	registers["PC"] = rPC // TODO: is this amd64-only?

	// Annoying aliases. TODO: amd64-specific.
	instructions["JB"] = amd64.AJCS
	instructions["JC"] = amd64.AJCS
	instructions["JNAE"] = amd64.AJCS
	instructions["JLO"] = amd64.AJCS
	instructions["JAE"] = amd64.AJCC
	instructions["JNB"] = amd64.AJCC
	instructions["JNC"] = amd64.AJCC
	instructions["JHS"] = amd64.AJCC
	instructions["JE"] = amd64.AJEQ
	instructions["JZ"] = amd64.AJEQ
	instructions["JNZ"] = amd64.AJNE
	instructions["JBE"] = amd64.AJLS
	instructions["JNA"] = amd64.AJLS
	instructions["JA"] = amd64.AJHI
	instructions["JNBE"] = amd64.AJHI
	instructions["JS"] = amd64.AJMI
	instructions["JNS"] = amd64.AJPL
	instructions["JP"] = amd64.AJPS
	instructions["JPE"] = amd64.AJPS
	instructions["JNP"] = amd64.AJPC
	instructions["JPO"] = amd64.AJPC
	instructions["JL"] = amd64.AJLT
	instructions["JNGE"] = amd64.AJLT
	instructions["JNL"] = amd64.AJGE
	instructions["JNG"] = amd64.AJLE
	instructions["JG"] = amd64.AJGT
	instructions["JNLE"] = amd64.AJGT
	instructions["MASKMOVDQU"] = amd64.AMASKMOVOU
	instructions["MOVD"] = amd64.AMOVQ
	instructions["MOVDQ2Q"] = amd64.AMOVQ

	for i, s := range amd64.Anames6 {
		instructions[s] = i
	}

	pseudos["DATA"] = amd64.ADATA
	pseudos["FUNCDATA"] = amd64.AFUNCDATA
	pseudos["GLOBL"] = amd64.AGLOBL
	pseudos["PCDATA"] = amd64.APCDATA
	pseudos["TEXT"] = amd64.ATEXT

	// These instructions write to prog.To.
	unaryDestination[amd64.ABSWAPL] = true
	unaryDestination[amd64.ABSWAPQ] = true
	unaryDestination[amd64.ACMPXCHG8B] = true
	unaryDestination[amd64.ADECB] = true
	unaryDestination[amd64.ADECL] = true
	unaryDestination[amd64.ADECQ] = true
	unaryDestination[amd64.ADECW] = true
	unaryDestination[amd64.AINCB] = true
	unaryDestination[amd64.AINCL] = true
	unaryDestination[amd64.AINCQ] = true
	unaryDestination[amd64.AINCW] = true
	unaryDestination[amd64.ANEGB] = true
	unaryDestination[amd64.ANEGL] = true
	unaryDestination[amd64.ANEGQ] = true
	unaryDestination[amd64.ANEGW] = true
	unaryDestination[amd64.ANOTB] = true
	unaryDestination[amd64.ANOTL] = true
	unaryDestination[amd64.ANOTQ] = true
	unaryDestination[amd64.ANOTW] = true
	unaryDestination[amd64.APOPL] = true
	unaryDestination[amd64.APOPQ] = true
	unaryDestination[amd64.APOPW] = true
	unaryDestination[amd64.ASETCC] = true
	unaryDestination[amd64.ASETCS] = true
	unaryDestination[amd64.ASETEQ] = true
	unaryDestination[amd64.ASETGE] = true
	unaryDestination[amd64.ASETGT] = true
	unaryDestination[amd64.ASETHI] = true
	unaryDestination[amd64.ASETLE] = true
	unaryDestination[amd64.ASETLS] = true
	unaryDestination[amd64.ASETLT] = true
	unaryDestination[amd64.ASETMI] = true
	unaryDestination[amd64.ASETNE] = true
	unaryDestination[amd64.ASETOC] = true
	unaryDestination[amd64.ASETOS] = true
	unaryDestination[amd64.ASETPC] = true
	unaryDestination[amd64.ASETPL] = true
	unaryDestination[amd64.ASETPS] = true
	unaryDestination[amd64.AFFREE] = true
	unaryDestination[amd64.AFLDENV] = true
	unaryDestination[amd64.AFSAVE] = true
	unaryDestination[amd64.AFSTCW] = true
	unaryDestination[amd64.AFSTENV] = true
	unaryDestination[amd64.AFSTSW] = true
	unaryDestination[amd64.AFXSAVE] = true
	unaryDestination[amd64.AFXSAVE64] = true
	unaryDestination[amd64.ASTMXCSR] = true
}

type Parser struct {
	lex           TokenReader
	lineNum       int
	errorLine     int   // Line number of last error.
	errorCount    int   // Number of errors.
	pc            int64 // virtual PC; count of Progs; doesn't advance for GLOBL or DATA.
	input         []LexToken
	inputPos      int
	pendingLabels []string // Labels to attach to next instruction.
	labels        map[string]*liblink.Prog
	toPatch       []Patch
	addr          []Addr //[]liblink.Addr
	arch          *liblink.LinkArch
	linkCtxt      *liblink.Link
	firstProg     *liblink.Prog
	lastProg      *liblink.Prog
}

type Patch struct {
	prog  *liblink.Prog
	label string
}

func NewParser(ctxt *liblink.Link, arch *liblink.LinkArch, lex TokenReader) *Parser {
	return &Parser{
		linkCtxt: ctxt,
		arch:     arch,
		lex:      lex,
		labels:   make(map[string]*liblink.Prog),
	}
}

func (p *Parser) errorf(format string, args ...interface{}) {
	if p.lineNum == p.errorLine {
		// Only one error per line.
		return
	}
	p.errorLine = p.lineNum
	// Put file and line information on head of message.
	format = "%s:%d: " + format + "\n"
	args = append([]interface{}{p.lex.FileName(), p.lineNum}, args...)
	fmt.Fprintf(os.Stderr, format, args...)
	p.errorCount++
	if p.errorCount > 10 {
		log.Fatal("too many errors")
	}
}

func (p *Parser) Parse() (*liblink.Prog, bool) {
	for p.line() {
	}
	if p.errorCount > 0 {
		return nil, false
	}
	p.patch()
	return p.firstProg, true
}

// WORD op {, op} '\n'
func (p *Parser) line() bool {
	// Skip newlines.
	var tok Token
	for {
		tok = p.lex.Next()
		// We save the line number here so error messages from this instruction
		// are labeled with this line. Otherwise we complain after we've absorbed
		// the terminating newline and the line numbers are off by one in errors.
		p.lineNum = p.lex.Line()
		switch tok {
		case '\n':
			continue
		case scanner.EOF:
			return false
		}
		break
	}
	// First item must be an identifier.
	if tok != scanner.Ident {
		p.errorf("expected identifier, found %q", p.lex.Text())
		return false // Might as well stop now.
	}
	word := p.lex.Text()
	operands := make([][]LexToken, 0, 3)
	// Zero or more comma-separated operands, one per loop.
	first := true // Permit ':' to define this as a label.
	for tok != '\n' && tok != ';' {
		// Process one operand.
		items := make([]LexToken, 0, 3)
		for {
			tok = p.lex.Next()
			if first {
				if tok == ':' {
					p.pendingLabels = append(p.pendingLabels, word)
					return true
				}
				first = false
			}
			if tok == scanner.EOF {
				p.errorf("unexpected EOF")
				return false
			}
			if tok == '\n' || tok == ';' || tok == ',' {
				break
			}
			items = append(items, LexToken{tok, p.lex.Text()})
		}
		if len(items) > 0 {
			operands = append(operands, items)
		} else if len(operands) > 0 {
			// Had a comma but nothing after.
			p.errorf("missing operand")
		}
	}
	i := pseudos[word]
	if i != 0 {
		p.pseudo(i, word, operands)
		return true
	}
	i = instructions[word]
	if i != 0 {
		p.instruction(i, word, operands)
		return true
	}
	p.errorf("unrecognized instruction %s", word)
	return true
}

func (p *Parser) instruction(op int, word string, operands [][]LexToken) {
	p.addr = p.addr[0:0]
	for _, op := range operands {
		p.addr = append(p.addr, p.address(op))
	}
	// Is it a jump? TODO
	if word[0] == 'J' || word == "CALL" {
		p.asmJump(op, p.addr)
		return
	}
	p.asmInstruction(op, p.addr)
}

func (p *Parser) pseudo(op int, word string, operands [][]LexToken) {
	switch op {
	case amd64.ATEXT:
		p.asmText(word, operands)
	case amd64.ADATA:
		p.asmData(word, operands)
	case amd64.AGLOBL:
		p.asmGlobl(word, operands)
	case amd64.APCDATA:
		p.asmPCData(word, operands)
	case amd64.AFUNCDATA:
		p.asmFuncData(word, operands)
	default:
		p.errorf("unimplemented: %s", word)
	}
}

func (p *Parser) start(operand []LexToken) {
	p.input = operand
	p.inputPos = 0
}

// address parses the operand into a link address structure.
func (p *Parser) address(operand []LexToken) Addr {
	p.start(operand)
	addr := Addr{}
	// addr.Typ = p.arch.D_NONE
	// addr.Index = p.arch.D_NONE
	p.operand(&addr)
	return addr
}

// parse (R). The opening paren is known to be there.
// The return value states whether it was a scaled mode.
func (p *Parser) parenRegister(a *Addr) bool {
	p.next()
	tok := p.next()
	if tok.Token != scanner.Ident {
		p.errorf("expected register, got %s", tok.text)
	}
	r, present := registers[tok.text]
	if !present {
		p.errorf("expected register, found %s", tok.text)
	}
	a.isIndirect = true
	scaled := p.peek() == '*'
	if scaled {
		// (R*2)
		p.next()
		tok := p.get(scanner.Int)
		a.scale = p.scale(tok.text)
		a.index = r
	} else {
		if a.hasRegister {
			p.errorf("multiple indirections")
		}
		a.hasRegister = true
		a.register = r
	}
	p.expect(')')
	p.next()
	return scaled
}

// scale converts a decimal string into a valid scale factor.
func (p *Parser) scale(s string) int8 {
	switch s {
	case "1", "2", "4", "8":
		return int8(s[0] - '0')
	}
	p.errorf("bad scale: %s", s)
	return 0
}

// parse (R) or (R)(R*scale). The opening paren is known to be there.
func (p *Parser) addressMode(a *Addr) {
	scaled := p.parenRegister(a)
	if !scaled && p.peek() == '(' {
		p.parenRegister(a)
	}
}

// operand parses a general operand and stores the result in *a.
func (p *Parser) operand(a *Addr) bool {
	if len(p.input) == 0 {
		p.errorf("empty operand: cannot happen")
		return false
	}
	switch p.peek() {
	case '$':
		p.next()
		switch p.peek() {
		case scanner.Ident:
			a.isImmediateAddress = true
			p.operand(a) // TODO
		case scanner.String:
			a.isImmediateConstant = true
			a.hasString = true
			a.string = p.atos(p.next().text)
		case scanner.Int, scanner.Float, '+', '-', '~', '(':
			a.isImmediateConstant = true
			if p.have(scanner.Float) {
				a.hasFloat = true
				a.float = p.floatExpr()
			} else {
				a.hasOffset = true
				a.offset = int64(p.expr())
			}
		default:
			p.errorf("illegal %s in immediate operand", p.next().text)
		}
	case '*':
		p.next()
		tok := p.next()
		r, present := registers[tok.text]
		if !present {
			p.errorf("expected register; got %s", tok.text)
		}
		a.hasRegister = true
		a.register = r
	case '(':
		p.next()
		if p.peek() == scanner.Ident {
			p.back()
			p.addressMode(a)
			break
		}
		p.back()
		fallthrough
	case '+', '-', '~', scanner.Int, scanner.Float:
		if p.have(scanner.Float) {
			a.hasFloat = true
			a.float = p.floatExpr()
		} else {
			a.hasOffset = true
			a.offset = int64(p.expr())
		}
		if p.peek() != scanner.EOF {
			p.expect('(')
			p.addressMode(a)
		}
	case scanner.Ident:
		tok := p.next()
		// Either R or (most general) ident<>+4(SB)(R*scale).
		if r, present := registers[tok.text]; present {
			a.hasRegister = true
			a.register = r
			// Possibly register pair: DX:AX.
			if p.peek() == ':' {
				p.next()
				tok = p.get(scanner.Ident)
				a.hasRegister2 = true
				a.register2 = registers[tok.text]
			}
			break
		}
		// Weirdness with statics: Might now have "<>".
		if p.peek() == '<' {
			p.next()
			p.get('>')
			a.isStatic = true
		}
		if p.peek() == '+' || p.peek() == '-' {
			a.hasOffset = true
			a.offset = int64(p.expr())
		}
		a.symbol = tok.text
		if p.peek() == scanner.EOF {
			break
		}
		// Expect (SB) or (FP)
		p.expect('(')
		p.parenRegister(a)
		if a.register != rSB && a.register != rFP && a.register != rSP {
			p.errorf("expected SB, FP, or SP offset for %s", tok.text)
		}
		// Possibly have scaled register (CX*8).
		if p.peek() != scanner.EOF {
			p.expect('(')
			p.addressMode(a)
		}
	default:
		p.errorf("unexpected %s in operand", p.next().text)
	}
	p.expect(scanner.EOF)
	return true
}

// expr = term | term '+' term
func (p *Parser) expr() uint64 {
	value := p.term()
	for {
		switch p.peek() {
		case '+':
			p.next()
			x := p.term()
			if addOverflows(x, value) {
				p.errorf("overflow in %d+%d", value, x)
			}
			value += x
		case '-':
			p.next()
			value -= p.term()
		case '|':
			p.next()
			value |= p.term()
		case '^':
			p.next()
			value ^= p.term()
		default:
			return value
		}
	}
}

// floatExpr = fconst | '-' floatExpr | '+' floatExpr | '(' floatExpr ')'
func (p *Parser) floatExpr() float64 {
	tok := p.next()
	switch tok.Token {
	case '(':
		v := p.floatExpr()
		if p.next().Token != ')' {
			p.errorf("missing closing paren")
		}
		return v
	case '+':
		return +p.floatExpr()
	case '-':
		return -p.floatExpr()
	case scanner.Float:
		return p.atof(tok.text)
	}
	p.errorf("unexpected %s evaluating float expression", tok.text)
	return 0
}

// term = const | term '*' term | '(' expr ')'
func (p *Parser) term() uint64 {
	tok := p.next()
	switch tok.Token {
	case '(':
		v := p.expr()
		if p.next().Token != ')' {
			p.errorf("missing closing paren")
		}
		return v
	case '+':
		return +p.term()
	case '-':
		return -p.term()
	case '~':
		return ^p.term()
	case scanner.Int:
		value := p.atoi(tok.text)
		for {
			switch p.peek() {
			case '*':
				p.next()
				value *= p.term() // OVERFLOW?
			case '/':
				p.next()
				value /= p.term()
			case '%':
				p.next()
				value %= p.term()
			case LSH:
				p.next()
				shift := p.term()
				if shift < 0 {
					p.errorf("negative left shift %d", shift)
				}
				value <<= uint(shift)
			case RSH:
				p.next()
				shift := p.term()
				if shift < 0 {
					p.errorf("negative right shift %d", shift)
				}
				value >>= uint(shift)
			case '&':
				p.next()
				value &= p.term()
			default:
				return value
			}
		}
	}
	p.errorf("unexpected %s evaluating expression", tok.text)
	return 0
}

func (p *Parser) atoi(str string) uint64 {
	value, err := strconv.ParseUint(str, 0, 64)
	if err != nil {
		p.errorf("%s", err)
	}
	return value
}

func (p *Parser) atof(str string) float64 {
	value, err := strconv.ParseFloat(str, 64)
	if err != nil {
		p.errorf("%s", err)
	}
	return value
}

func (p *Parser) atos(str string) string {
	value, err := strconv.Unquote(str)
	if err != nil {
		p.errorf("%s", err)
	}
	return value
}

var end = LexToken{scanner.EOF, "end"}

func (p *Parser) next() LexToken {
	if !p.more() {
		return end
	}
	tok := p.input[p.inputPos]
	p.inputPos++
	return tok
}

func (p *Parser) back() {
	p.inputPos--
}

func (p *Parser) peek() Token {
	if p.more() {
		return p.input[p.inputPos].Token
	}
	return scanner.EOF
}

func (p *Parser) more() bool {
	return p.inputPos < len(p.input)
}

// get verifies that the next item has the expected type and returns it.
func (p *Parser) get(expected Token) LexToken {
	p.expect(expected)
	return p.next()
}

// expect verifies that the next item has the expected type. It does not consume it.
func (p *Parser) expect(expected Token) {
	if p.peek() != expected {
		p.errorf("expected %s, found %s", expected, p.next().text)
	}
}

// have reports whether the remaining tokens contain the specified token.
func (p *Parser) have(token Token) bool {
	for i := p.inputPos; i < len(p.input); i++ {
		if p.input[i].Token == token {
			return true
		}
	}
	return false
}
