// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"strings"
	"text/scanner"

	"code.google.com/p/rsc/c2go/liblink"
	"code.google.com/p/rsc/c2go/liblink/amd64"
)

type Addr struct {
	isStatic            bool    // symbol<>
	isImmediateConstant bool    // $3
	isImmediateAddress  bool    // $main·main(SB)
	isIndirect          bool    // (R1)
	hasRegister         bool    // register is set
	hasRegister2        bool    // register2 is set
	hasFloat            bool    // float is set
	hasOffset           bool    // offset is set
	hasString           bool    // string is set
	symbol              string  // "main·main"
	register            int     // R1
	register2           int     // R1 in R0:R1
	offset              int64   // 3
	float               float64 // 1.0e2 (floating constant)
	string              string  // "hi" (string constant)
	index               int     // R1 in (R1*8)
	scale               int8    // 8 in (R1*8)
}

const (
	// isStatic does not appear here; is and has methods ignore it.
	addrImmediateConstant = 1 << iota
	addrImmediateAddress
	addrIndirect
	addrSymbol
	addrRegister
	addrRegister2
	addrOffset
	addrFloat
	addrString
	addrIndex
	addrScale
)

// has reports whether the address has any of the specified elements.
// Indirect and immediate are not checked.
func (a *Addr) has(mask int) bool {
	if mask&addrSymbol != 0 && a.symbol != "" {
		return true
	}
	if mask&addrRegister != 0 && a.hasRegister {
		return true
	}
	if mask&addrRegister2 != 0 && a.hasRegister2 {
		return true
	}
	if mask&addrOffset != 0 && a.hasOffset {
		return true
	}
	if mask&addrFloat != 0 && a.hasFloat {
		return true
	}
	if mask&addrString != 0 && a.hasString {
		return true
	}
	if mask&addrIndex != 0 && a.index != 0 {
		return true
	}
	if mask&addrScale != 0 && a.scale != 0 {
		return true
	}
	return false
}

// has reports whether the address has exactly the specified elements.
// Indirect and immediate are checked.
func (a *Addr) is(mask int) bool {
	if (mask&addrImmediateConstant == 0) != !a.isImmediateConstant {
		return false
	}
	if (mask&addrImmediateAddress == 0) != !a.isImmediateAddress {
		return false
	}
	if (mask&addrIndirect == 0) != !a.isIndirect {
		return false
	}
	if (mask&addrSymbol == 0) != (a.symbol == "") {
		return false
	}
	if (mask&addrRegister == 0) != !a.hasRegister {
		return false
	}
	if (mask&addrRegister2 == 0) != !a.hasRegister2 {
		return false
	}
	if (mask&addrOffset == 0) != !a.hasOffset {
		// $0 has the immediate bit but value 0.
		return false
	}
	if (mask&addrFloat == 0) != !a.hasFloat {
		return false
	}
	if (mask&addrString == 0) != !a.hasString {
		return false
	}
	if (mask&addrIndex == 0) != (a.index == 0) {
		return false
	}
	if (mask&addrScale == 0) != (a.scale == 0) {
		return false
	}
	return true
}

// symbolType returns the extern/static etc. type appropriate for the symbol.
func (p *Parser) symbolType(a *Addr) int {
	switch a.register {
	case rFP:
		return amd64.D_PARAM
	case rSP:
		return amd64.D_AUTO
	case rSB:
		// See comment in addrToAddr.
		if a.isImmediateAddress {
			return amd64.D_ADDR
		}
		if a.isStatic {
			return amd64.D_STATIC
		}
		return amd64.D_EXTERN
	}
	p.errorf("invalid register for symbol %s", a.symbol)
	return 0
}

// TODO: configure the architecture

var noAddr = liblink.Addr{
	Typ:   amd64.D_NONE,
	Index: amd64.D_NONE,
}

func (p *Parser) addrToAddr(a *Addr) liblink.Addr {
	out := noAddr
	if a.has(addrSymbol) {
		// How to encode the symbols:
		// syntax = Typ,Index
		// $a(SB) = ADDR,EXTERN
		// $a<>(SB) = ADDR,STATIC
		// a(SB) = EXTERN,NONE
		// a<>(SB) = STATIC,NONE
		// The call to symbolType does the first column; we need to fix up Index here.
		out.Typ = p.symbolType(a)
		out.Sym = liblink.Linklookup(p.linkCtxt, a.symbol, 0)
		if a.isImmediateAddress {
			// Index field says whether it's a static.
			switch a.register {
			case rSB:
				if a.isStatic {
					out.Index = amd64.D_STATIC
				} else {
					out.Index = amd64.D_EXTERN
				}
			default:
				p.errorf("can't handle immediate address of %s not (SB)\n", a.symbol)
			}
		}
	} else if a.has(addrRegister) {
		// TODO: SP is tricky, and this isn't good enough.
		// SP = D_SP
		// 4(SP) = 4(D_SP)
		// x+4(SP) = D_AUTO with sym=x TODO
		out.Typ = a.register
		if a.register == rSP {
			out.Typ = amd64.D_SP
		}
		if a.isIndirect {
			out.Typ += amd64.D_INDIR
		}
		// a.register2 handled in the instruction method; it's bizarre.
	}
	if a.has(addrIndex) {
		out.Index = a.index
	}
	if a.has(addrScale) {
		out.Scale = a.scale
	}
	if a.has(addrOffset) {
		out.Offset = a.offset
		if a.is(addrOffset) {
			// RHS of MOVL $0xf1, 0xf1  // crash
			out.Typ = amd64.D_INDIR + amd64.D_NONE
		} else if a.isImmediateConstant && out.Typ == amd64.D_NONE {
			out.Typ = amd64.D_CONST
		}
	}
	if a.has(addrFloat) {
		out.U.Dval = a.float
		out.Typ = amd64.D_FCONST
	}
	if a.has(addrString) {
		out.U.Sval = a.string
		out.Typ = amd64.D_SCONST
	}
	// HACK TODO
	if a.isIndirect && !a.has(addrRegister) && a.has(addrIndex) {
		// LHS of LEAQ	0(BX*8), CX
		out.Typ = amd64.D_INDIR + amd64.D_NONE
	}
	return out
}

func (p *Parser) link(prog *liblink.Prog, doLabel bool) {
	if p.firstProg == nil {
		p.firstProg = prog
	} else {
		p.lastProg.Link = prog
	}
	p.lastProg = prog
	if doLabel {
		p.pc++
		for _, label := range p.pendingLabels {
			if p.labels[label] != nil {
				p.errorf("label %q multiply defined", label)
			}
			p.labels[label] = prog
		}
		p.pendingLabels = p.pendingLabels[0:0]
	}
	prog.Pc = int64(p.pc)
	// fmt.Println(p.lineNum, prog)
}

// asmText assembles a TEXT pseudo-op.
// TEXT runtime·sigtramp(SB),4,$0-0
func (p *Parser) asmText(word string, operands [][]LexToken) {
	if len(operands) != 3 {
		p.errorf("expect three operands for TEXT")
	}

	// Operand 0 is the symbol name in the form foo(SB).
	// That means symbol plus indirect on SB and no offset.
	nameAddr := p.address(operands[0])
	if !nameAddr.is(addrSymbol|addrRegister|addrIndirect) || nameAddr.register != rSB {
		p.errorf("TEXT symbol %q must be an offset from SB", nameAddr.symbol)
	}
	name := strings.Replace(nameAddr.symbol, "·", ".", 1)

	// Operand 1 is the text flag, a literal integer.
	flagAddr := p.address(operands[1])
	if !flagAddr.is(addrOffset) {
		p.errorf("TEXT flag for %s must be an integer", name)
	}
	flag := int8(flagAddr.offset)

	// Operand 2 is the frame and arg size.
	// Bizarre syntax: $a-b is two words, not subtraction.
	// We might even see $-b, which means $0-b. Ugly.
	// Assume if it has this syntax that b is a plain constant.
	// Not clear we can do better, but it doesn't matter.
	op := operands[2]
	n := len(op)
	var locals int64
	if n >= 2 && op[n-2].Token == '-' && op[n-1].Token == scanner.Int {
		p.start(op[n-1:])
		locals = int64(p.expr())
		op = op[:n-2]
	}
	args := int64(0)
	if len(op) == 1 && op[0].Token == '$' {
		// Special case for $-8.
		// Done; args is zero.
	} else {
		argsAddr := p.address(op)
		if !argsAddr.is(addrImmediateConstant | addrOffset) {
			p.errorf("TEXT frame size for %s must be an immediate constant", name)
		}
		args = argsAddr.offset
	}

	prog := &liblink.Prog{
		Ctxt:   p.linkCtxt,
		As:     amd64.ATEXT,
		Lineno: p.lineNum,
		From: liblink.Addr{
			Typ:   p.symbolType(&nameAddr),
			Index: amd64.D_NONE,
			Sym:   liblink.Linklookup(p.linkCtxt, name, 0),
			Scale: flag,
		},
		To: liblink.Addr{
			Typ:    amd64.D_CONST,
			Index:  amd64.D_NONE,
			Offset: (locals << 32) | args,
		},
	}
	p.link(prog, true)
}

// asmData assembles a DATA pseudo-op.
// DATA masks<>+0x00(SB)/4, $0x00000000
func (p *Parser) asmData(word string, operands [][]LexToken) {
	if len(operands) != 2 {
		p.errorf("expect two operands for DATA")
	}

	// Operand 0 has the general form foo<>+0x04(SB)/4.
	op := operands[0]
	n := len(op)
	if n < 3 || op[n-2].Token != '/' || op[n-1].Token != scanner.Int {
		p.errorf("expect /size for DATA argument")
	}
	scale := p.scale(op[n-1].text)
	op = op[:n-2]
	nameAddr := p.address(op)
	ok := nameAddr.is(addrSymbol|addrRegister|addrIndirect) || nameAddr.is(addrSymbol|addrRegister|addrIndirect|addrOffset)
	if !ok || nameAddr.register != rSB {
		p.errorf("DATA symbol %q must be an offset from SB", nameAddr.symbol)
	}
	name := strings.Replace(nameAddr.symbol, "·", ".", 1)

	// Operand 1 is an immediate constant or address.
	valueAddr := p.address(operands[1])
	if !valueAddr.isImmediateConstant && !valueAddr.isImmediateAddress {
		p.errorf("DATA value must be an immediate constant or address")
	}

	prog := &liblink.Prog{
		Ctxt:   p.linkCtxt,
		As:     amd64.ADATA,
		Lineno: p.lineNum,
		From: liblink.Addr{
			Typ:    p.symbolType(&nameAddr),
			Index:  amd64.D_NONE,
			Sym:    liblink.Linklookup(p.linkCtxt, name, 0),
			Offset: nameAddr.offset,
			Scale:  scale,
		},
		To: p.addrToAddr(&valueAddr),
	}
	p.link(prog, false)
}

// asmGlobl assembles a GLOBL pseudo-op.
// GLOBL shifts<>(SB),8,$256
// GLOBL shifts<>(SB),$256
func (p *Parser) asmGlobl(word string, operands [][]LexToken) {
	if len(operands) != 2 && len(operands) != 3 {
		p.errorf("expect two or three operands for GLOBL")
	}

	// Operand 0 has the general form foo<>+0x04(SB).
	nameAddr := p.address(operands[0])
	if !nameAddr.is(addrSymbol|addrRegister|addrIndirect) || nameAddr.register != rSB {
		p.errorf("GLOBL symbol %q must be an offset from SB", nameAddr.symbol)
	}
	name := strings.Replace(nameAddr.symbol, "·", ".", 1)

	// If three operands, middle operand is a scale.
	scale := int8(0)
	op := operands[1]
	if len(operands) == 3 {
		scaleAddr := p.address(op)
		if !scaleAddr.is(addrOffset) {
			p.errorf("GLOBL scale must be a constant")
		}
		scale = int8(scaleAddr.offset)
		op = operands[2]
	}

	// Final operand is an immediate constant.
	sizeAddr := p.address(op)
	if !sizeAddr.is(addrImmediateConstant | addrOffset) {
		p.errorf("GLOBL size must be an immediate constant")
	}
	size := sizeAddr.offset

	// log.Printf("GLOBL %s %d, $%d", name, scale, size)
	prog := &liblink.Prog{
		Ctxt:   p.linkCtxt,
		As:     amd64.AGLOBL,
		Lineno: p.lineNum,
		From: liblink.Addr{
			Typ:    p.symbolType(&nameAddr),
			Index:  amd64.D_NONE,
			Sym:    liblink.Linklookup(p.linkCtxt, name, 0),
			Offset: nameAddr.offset,
			Scale:  scale,
		},
		To: liblink.Addr{
			Typ:    amd64.D_CONST,
			Index:  amd64.D_NONE,
			Offset: size,
		},
	}
	p.link(prog, false)
}

// asmPCData assembles a PCDATA pseudo-op.
// PCDATA $2, $705
func (p *Parser) asmPCData(word string, operands [][]LexToken) {
	if len(operands) != 2 {
		p.errorf("expect two operands for PCDATA")
	}

	// Operand 0 must be an immediate constant.
	addr0 := p.address(operands[0])
	if !addr0.is(addrImmediateConstant | addrOffset) {
		p.errorf("PCDATA value must be an immediate constant")
	}
	value0 := addr0.offset

	// Operand 1 must be an immediate constant.
	addr1 := p.address(operands[1])
	if !addr1.is(addrImmediateConstant | addrOffset) {
		p.errorf("PCDATA value must be an immediate constant")
	}
	value1 := addr1.offset

	// log.Printf("PCDATA $%d, $%d", value0, value1)
	prog := &liblink.Prog{
		Ctxt:   p.linkCtxt,
		As:     amd64.APCDATA,
		Lineno: p.lineNum,
		From: liblink.Addr{
			Typ:    amd64.D_CONST,
			Index:  amd64.D_NONE,
			Offset: value0,
		},
		To: liblink.Addr{
			Typ:    amd64.D_CONST,
			Index:  amd64.D_NONE,
			Offset: value1,
		},
	}
	p.link(prog, true)
}

// asmFuncData assembles a FUNCDATA pseudo-op.
// FUNCDATA $1, funcdata<>+4(SB)
func (p *Parser) asmFuncData(word string, operands [][]LexToken) {
	if len(operands) != 2 {
		p.errorf("expect two operands for FUNCDATA")
	}

	// Operand 0 must be an immediate constant.
	valueAddr := p.address(operands[0])
	if !valueAddr.is(addrImmediateConstant | addrOffset) {
		p.errorf("FUNCDATA value must be an immediate constant")
	}
	value := valueAddr.offset

	// Operand 1 is a symbol name in the form foo(SB).
	// That means symbol plus indirect on SB and no offset.
	nameAddr := p.address(operands[1])
	if !nameAddr.is(addrSymbol|addrRegister|addrIndirect) || nameAddr.register != rSB {
		p.errorf("FUNCDATA symbol %q must be an offset from SB", nameAddr.symbol)
	}
	name := strings.Replace(nameAddr.symbol, "·", ".", 1)

	// log.Printf("FUNCDATA %s, $%d", name, value)
	prog := &liblink.Prog{
		Ctxt:   p.linkCtxt,
		As:     amd64.AFUNCDATA,
		Lineno: p.lineNum,
		From: liblink.Addr{
			Typ:    amd64.D_CONST,
			Index:  amd64.D_NONE,
			Offset: value,
		},
		To: liblink.Addr{
			Typ:   p.symbolType(&nameAddr),
			Index: amd64.D_NONE,
			Sym:   liblink.Linklookup(p.linkCtxt, name, 0),
		},
	}
	p.link(prog, true)
}

// asmJump assembles a jump instruction.
// JMP	R1
// JMP	exit
// JMP	3(PC)
func (p *Parser) asmJump(op int, addr []Addr) {
	var target *Addr
	switch len(addr) {
	default:
		p.errorf("jump must have one or two addresses")
	case 1:
		target = &addr[0]
	case 2:
		if !addr[0].is(0) {
			p.errorf("two-address jump must have empty first address")
		}
		target = &addr[1]
	}
	prog := &liblink.Prog{
		Lineno: p.lineNum,
		Ctxt:   p.linkCtxt,
		As:     op,
		From:   noAddr,
	}
	switch {
	case target.is(addrRegister):
		// JMP R1
		prog.To = p.addrToAddr(target)
	case target.is(addrSymbol):
		// JMP exit
		targetProg := p.labels[target.symbol]
		if targetProg == nil {
			p.toPatch = append(p.toPatch, Patch{prog, target.symbol})
		} else {
			p.branch(prog, targetProg)
		}
	case target.is(addrRegister | addrIndirect), target.is(addrRegister | addrIndirect | addrOffset):
		// JMP 4(AX)
		if target.register == rPC {
			prog.To = liblink.Addr{
				Typ:    amd64.D_BRANCH,
				Index:  amd64.D_NONE,
				Offset: p.pc + 1 + target.offset, // +1 because p.pc is incremented in link, below.
			}
		} else {
			prog.To = p.addrToAddr(target)
		}
	case target.is(addrSymbol | addrIndirect | addrRegister):
		// JMP main·morestack(SB)
		if target.register != rSB {
			p.errorf("jmp to symbol must be SB-relative")
		}
		prog.To = liblink.Addr{
			Typ:    amd64.D_BRANCH,
			Sym:    liblink.Linklookup(p.linkCtxt, target.symbol, 0),
			Index:  amd64.D_NONE,
			Offset: target.offset,
		}
	default:
		p.errorf("cannot assemble jump %+v", target)
	}
	p.link(prog, true)
}

func (p *Parser) patch() {
	for _, patch := range p.toPatch {
		targetProg := p.labels[patch.label]
		if targetProg == nil {
			p.errorf("undefined label %s", patch.label)
		} else {
			p.branch(patch.prog, targetProg)
		}
	}
}

func (p *Parser) branch(jmp, target *liblink.Prog) {
	jmp.To = liblink.Addr{
		Typ:   amd64.D_BRANCH,
		Index: amd64.D_NONE,
	}
	jmp.To.U.Branch = target
}

// asmInstruction assembles an instruction.
// MOVW R9, (R10)
func (p *Parser) asmInstruction(op int, addr []Addr) {
	prog := &liblink.Prog{
		Lineno: p.lineNum,
		Ctxt:   p.linkCtxt,
		As:     op,
	}
	switch len(addr) {
	case 0:
		prog.From = noAddr
		prog.To = noAddr
	case 1:
		if unaryDestination[op] {
			prog.From = noAddr
			prog.To = p.addrToAddr(&addr[0])
		} else {
			prog.From = p.addrToAddr(&addr[0])
			prog.To = noAddr
		}
	case 2:
		prog.From = p.addrToAddr(&addr[0])
		prog.To = p.addrToAddr(&addr[1])
		// DX:AX as a register pair can only appear on the RHS.
		// Bizarrely, to liblink it's specified by setting index on the LHS.
		// TODO: can we fix this?
		if addr[1].has(addrRegister2) {
			if prog.From.Index != amd64.D_NONE {
				p.errorf("register pair operand on RHS must have register on LHS")
			}
			prog.From.Index = addr[1].register2
		}
	case 3:
		// CMPSD etc.; third operand is imm8, stored in offset, or a register.
		prog.From = p.addrToAddr(&addr[0])
		prog.To = p.addrToAddr(&addr[1])
		switch {
		case addr[2].is(addrOffset):
			prog.To.Offset = addr[2].offset
		case addr[2].is(addrRegister):
			// Strange reodering.
			prog.To = p.addrToAddr(&addr[2])
			prog.From = p.addrToAddr(&addr[1])
			if !addr[0].isImmediateConstant {
				p.errorf("expected $value for 1st operand")
			}
			prog.To.Offset = addr[0].offset
		default:
			p.errorf("expected offset or register for 3rd operand")
		}
	default:
		p.errorf("can't handle instruction with %d operands", len(addr))
	}
	p.link(prog, true)
}
