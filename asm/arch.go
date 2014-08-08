// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"

	"code.google.com/p/rsc/c2go/liblink"
	"code.google.com/p/rsc/c2go/liblink/amd64"
	"code.google.com/p/rsc/c2go/liblink/x86"
)

// Arch wraps the link architecture object with more architecture-specific information
type Arch struct {
	*liblink.LinkArch
	D_INDIR          int // TODO: why not in LinkArch?
	D_CONST2         int // TODO: why not in LinkArch?
	SP               int
	noAddr           liblink.Addr
	instructions     map[string]int
	registers        map[string]int
	pseudos          map[string]int // TEXT, DATA etc.
	unaryDestination map[int]bool   // Instruction takes one operand and result is a destination.
}

func setArch(GOARCH string) *Arch {
	// TODO: Is this how to set this up?
	switch GOARCH {
	case "386":
		return arch386()
	case "amd64":
		return archAmd64()
	}
	log.Fatalf("unrecognized architecture %s", GOARCH)
	return nil
}

func arch386() *Arch {
	noAddr := liblink.Addr{
		Typ:   x86.D_NONE,
		Index: x86.D_NONE,
	}

	registers := make(map[string]int)
	// Create maps for easy lookup of instruction names etc.
	// TODO: Should this be done in liblink for us?
	for i, s := range x86.Regstr {
		registers[s] = i
	}
	// Pseudo-registers.
	registers["SB"] = rSB
	registers["FP"] = rFP
	registers["SP"] = rSP
	registers["PC"] = rPC

	instructions := make(map[string]int)
	for i, s := range x86.Anames8 {
		instructions[s] = i
	}
	// Annoying aliases.
	instructions["JA"] = x86.AJHI
	instructions["JAE"] = x86.AJCC
	instructions["JB"] = x86.AJCS
	instructions["JBE"] = x86.AJLS
	instructions["JC"] = x86.AJCS
	instructions["JE"] = x86.AJEQ
	instructions["JG"] = x86.AJGT
	instructions["JHS"] = x86.AJCC
	instructions["JL"] = x86.AJLT
	instructions["JLO"] = x86.AJCS
	instructions["JNA"] = x86.AJLS
	instructions["JNAE"] = x86.AJCS
	instructions["JNB"] = x86.AJCC
	instructions["JNBE"] = x86.AJHI
	instructions["JNC"] = x86.AJCC
	instructions["JNG"] = x86.AJLE
	instructions["JNGE"] = x86.AJLT
	instructions["JNL"] = x86.AJGE
	instructions["JNLE"] = x86.AJGT
	instructions["JNO"] = x86.AJOC
	instructions["JNP"] = x86.AJPC
	instructions["JNS"] = x86.AJPL
	instructions["JNZ"] = x86.AJNE
	instructions["JO"] = x86.AJOS
	instructions["JP"] = x86.AJPS
	instructions["JPE"] = x86.AJPS
	instructions["JPO"] = x86.AJPC
	instructions["JS"] = x86.AJMI
	instructions["JZ"] = x86.AJEQ
	instructions["MASKMOVDQU"] = x86.AMASKMOVOU
	instructions["MOVOA"] = x86.AMOVO
	instructions["MOVNTDQ"] = x86.AMOVNTO

	pseudos := make(map[string]int) // TEXT, DATA etc.
	pseudos["DATA"] = x86.ADATA
	pseudos["FUNCDATA"] = x86.AFUNCDATA
	pseudos["GLOBL"] = x86.AGLOBL
	pseudos["PCDATA"] = x86.APCDATA
	pseudos["TEXT"] = x86.ATEXT

	unaryDestination := make(map[int]bool) // Instruction takes one operand and result is a destination.
	// These instructions write to prog.To.
	unaryDestination[x86.ABSWAPL] = true
	unaryDestination[x86.ACMPXCHG8B] = true
	unaryDestination[x86.ADECB] = true
	unaryDestination[x86.ADECL] = true
	unaryDestination[x86.ADECW] = true
	unaryDestination[x86.AINCB] = true
	unaryDestination[x86.AINCL] = true
	unaryDestination[x86.AINCW] = true
	unaryDestination[x86.ANEGB] = true
	unaryDestination[x86.ANEGL] = true
	unaryDestination[x86.ANEGW] = true
	unaryDestination[x86.ANOTB] = true
	unaryDestination[x86.ANOTL] = true
	unaryDestination[x86.ANOTW] = true
	unaryDestination[x86.APOPL] = true
	unaryDestination[x86.APOPW] = true
	unaryDestination[x86.ASETCC] = true
	unaryDestination[x86.ASETCS] = true
	unaryDestination[x86.ASETEQ] = true
	unaryDestination[x86.ASETGE] = true
	unaryDestination[x86.ASETGT] = true
	unaryDestination[x86.ASETHI] = true
	unaryDestination[x86.ASETLE] = true
	unaryDestination[x86.ASETLS] = true
	unaryDestination[x86.ASETLT] = true
	unaryDestination[x86.ASETMI] = true
	unaryDestination[x86.ASETNE] = true
	unaryDestination[x86.ASETOC] = true
	unaryDestination[x86.ASETOS] = true
	unaryDestination[x86.ASETPC] = true
	unaryDestination[x86.ASETPL] = true
	unaryDestination[x86.ASETPS] = true
	unaryDestination[x86.AFFREE] = true
	unaryDestination[x86.AFLDENV] = true
	unaryDestination[x86.AFSAVE] = true
	unaryDestination[x86.AFSTCW] = true
	unaryDestination[x86.AFSTENV] = true
	unaryDestination[x86.AFSTSW] = true

	return &Arch{
		LinkArch:         &x86.Link386,
		D_INDIR:          x86.D_INDIR,
		D_CONST2:         x86.D_CONST2,
		SP:               x86.D_SP,
		noAddr:           noAddr,
		instructions:     instructions,
		registers:        registers,
		pseudos:          pseudos,
		unaryDestination: unaryDestination,
	}
}

func archAmd64() *Arch {
	noAddr := liblink.Addr{
		Typ:   amd64.D_NONE,
		Index: amd64.D_NONE,
	}

	registers := make(map[string]int)
	// Create maps for easy lookup of instruction names etc.
	// TODO: Should this be done in liblink for us?
	for i, s := range amd64.Regstr {
		registers[s] = i
	}
	// Pseudo-registers.
	registers["SB"] = rSB
	registers["FP"] = rFP
	registers["SP"] = rSP
	registers["PC"] = rPC

	instructions := make(map[string]int)
	for i, s := range amd64.Anames6 {
		instructions[s] = i
	}
	// Annoying aliases.
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

	pseudos := make(map[string]int) // TEXT, DATA etc.
	pseudos["DATA"] = amd64.ADATA
	pseudos["FUNCDATA"] = amd64.AFUNCDATA
	pseudos["GLOBL"] = amd64.AGLOBL
	pseudos["PCDATA"] = amd64.APCDATA
	pseudos["TEXT"] = amd64.ATEXT

	unaryDestination := make(map[int]bool) // Instruction takes one operand and result is a destination.
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

	return &Arch{
		LinkArch:         &amd64.Linkamd64,
		D_INDIR:          amd64.D_INDIR,
		D_CONST2:         amd64.D_NONE,
		SP:               amd64.D_SP,
		noAddr:           noAddr,
		instructions:     instructions,
		registers:        registers,
		pseudos:          pseudos,
		unaryDestination: unaryDestination,
	}
}
