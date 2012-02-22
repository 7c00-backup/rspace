package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

var doNum = flag.Bool("n", false, "output numeric values")
var doChar = flag.Bool("c", false, "output characters")
var doText = flag.Bool("t", false, "output plain text")
var doDesc = flag.Bool("d", false, "describe the characters from the Unicode database")

var printRange = false

func main() {
	flag.Usage = usage
	flag.Parse()
	mode()
	var codes []rune
	switch {
	case *doChar:
		codes = argsAreNumbers()
	case *doNum:
		codes = argsAreChars()
	}
	if *doDesc {
		desc(codes)
		return
	}
	if *doText {
		fmt.Printf("%s\n", string(codes))
		return
	}
	b := new(bytes.Buffer)
	for i, c := range codes {
		switch {
		case printRange:
			fmt.Fprintf(b, "%.4x %c", c, c)
			if i % 4 ==  3 {
				fmt.Fprint(b, "\n")
			} else {
				fmt.Fprint(b, "\t")
			}
		case *doChar:
			fmt.Fprintf(b, "%c\n", c)
		case *doNum:
			fmt.Fprintf(b, "%.4x\n", c)
		}
	}
	if b.Len() > 0 && b.Bytes()[b.Len()-1] != '\n' {
		fmt.Fprint(b, "\n")
	}
	fmt.Print(b)
}

const usageText = `usage: unicode [-c] [-d] [-n] [-t]
-c: input is hex; output characters (xyz)
-n: input is characters; output hex (23 or 23-44)
-d: output textual description
-t: output plain text, not one char per line

Default behavior sniffs the arguments to select -c vs. -n.
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(2)
}

// Mode determines whether we have numeric or character input.
// If there are no flags, we sniff the first argument.
func mode() {
	if *doNum || *doChar {
		return
	}
	if len(flag.Args()) == 0 {
		usage()
	}
	// If first arg is a range, print chars from hex.
	if strings.ContainsRune(flag.Arg(0), '-') {
		*doChar = true
		return
	}
	// If there are non-hex digits, print hex from chars.
	for _, r := range strings.Join(flag.Args(), "") {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			*doNum = true
			return
		}
	}
	*doChar = true
}

func argsAreChars() []rune {
	var codes []rune
	for i, a := range flag.Args() {
		for _, r := range a {
			codes = append(codes, r)
		}
		// Add space between arguments if output is plain text.
		if *doText && i < len(flag.Args())-1 {
			codes = append(codes, ' ')
		}
	}
	return codes
}

func parseRune(s string) rune {
	r, err := strconv.ParseInt(s, 16, 22)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(2)
	}
	return rune(r)
}

func argsAreNumbers() []rune {
	var codes []rune
	for _, a := range flag.Args() {
		if s := strings.Split(a, "-"); len(s) == 2 {
			printRange = true
			r1 := parseRune(s[0])
			r2 := parseRune(s[1])
			if r2 < r1 {
				usage()
			}
			for  ; r1 <= r2; r1++ {
				codes = append(codes, r1)
			}
			continue
		}
		codes = append(codes, parseRune(a))
	}
	return codes
}

func desc(codes []rune) {
	text, err := ioutil.ReadFile("/usr/local/plan9/lib/unicode")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(2)
	}
	lines := strings.Split(string(text), "\n")
	runeData := make(map[rune]string)
	for i, l := range lines {
		if len(l) == 0 {
			break
		}
		tab := strings.IndexRune(l, '\t')
		if tab < 0 {
			fmt.Fprintf(os.Stderr, "malformed database at line %d\n", i)
			os.Exit(2)
		}
		runeData[parseRune(l[0:tab])] = l[tab+1:]
	}
	for _, r := range codes {
		fmt.Printf("%#U %s\n", r, runeData[r])
	}
}
