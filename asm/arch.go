// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"

	"code.google.com/p/rsc/c2go/liblink"
	"code.google.com/p/rsc/c2go/liblink/amd64"
)

// TODO: configure the architecture

// Arch wraps the link architecture object with more architecture-specific information
type Arch struct {
	*liblink.LinkArch
	D_INDIR          int // TODO: why not in LinkArch?
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
	case "amd64":
		return archAmd64()
		/*
			case "amd64p32":
				arch = &amd64.Linkamd64p32
			case "386":
				arch = &x86.Link386
			case "arm":
				arch = &arm.Linkarm
		*/
	}
	log.Fatalf("unrecognized architecture %s", GOARCH)
	return nil
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
	registers["SP"] = rSP // TODO: is this amd64-only?
	registers["PC"] = rPC // TODO: is this amd64-only?

	instructions := make(map[string]int)
	for i, s := range amd64.Anames6 {
		instructions[s] = i
	}
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
		SP:               amd64.D_SP,
		noAddr:           noAddr,
		instructions:     instructions,
		registers:        registers,
		pseudos:          pseudos,
		unaryDestination: unaryDestination,
	}
}
