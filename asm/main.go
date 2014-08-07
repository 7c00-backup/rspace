// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"strings"

	"code.google.com/p/rsc/c2go/liblink"
)

var (
	outputFile = flag.String("o", "", "output file; default foo.6 for /a/b/c/foo.s on arm64 (unused TODO)")
	printOut   = flag.Bool("S", true, "print assembly and machine code") // TODO: set to false
	trimPath   = flag.String("trimpath", "", "remove prefix from recorded source file paths (unused TODO)")
)

func init() {
	flag.Var(&dFlag, "D", "predefined symbol with optional simple value -D=identifer=value; can be set multiple times")
	flag.Var(&iFlag, "I", "include directory; can be set multiple times")
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("asm: ")

	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}

	arch := setArch(build.Default.GOARCH)

	// Flag refinement.
	if *outputFile == "" {
		input := filepath.Base(flag.Arg(0))
		if strings.HasSuffix(input, ".s") {
			input = input[:len(input)-2]
		}
		*outputFile = fmt.Sprintf("%s.%c", input, arch.Thechar)
	}

	// Create object file, write header.
	fd, err := os.Create(*outputFile)
	if err != nil {
		log.Fatal(err)
	}
	ctxt := liblink.Linknew(arch.LinkArch)
	if *printOut {
		ctxt.Debugasm = 1
	}
	ctxt.Bso = liblink.Binitw(os.Stdout)
	defer liblink.Bflush(ctxt.Bso)
	ctxt.Diag = log.Fatalf
	output := liblink.Binitw(fd)
	liblink.Bprint(output, "go object %s %s %s\n", liblink.Getgoos(), liblink.Getgoarch(), liblink.Getgoversion())
	liblink.Bprint(output, "!\n")

	lexer := NewLexer(flag.Arg(0), ctxt, dFlag, iFlag)
	parser := NewParser(ctxt, arch, lexer)
	pList := liblink.Linknewplist(ctxt)
	var ok bool
	pList.Firstpc, ok = parser.Parse()
	if !ok {
		log.Print("FAIL TODO")
		os.Exit(1)
	}
	liblink.Writeobj(ctxt, output)
	liblink.Bflush(output)
	log.Print("OK")
}

var (
	dFlag multiFlag
	iFlag multiFlag
)

// multiFlag allows setting a value multiple times to collect a list, as in -I=dir1 -I=dir2.
type multiFlag []string

func (m *multiFlag) String() string {
	return fmt.Sprint(*m)
}

func (m *multiFlag) Set(val string) error {
	(*m) = append(*m, val)
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: asm [options] file.s\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
	os.Exit(2)
}
