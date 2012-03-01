// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*

Slide files have the following format.  The first non-blank non-comment
line is the title, so the header looks like

	Title of presentation
	Which may be multiple lines
	<blank line>
	Several lines of
	Text such as the Author,
	Date,
	Venue,
	etc.

Only the title is treated specially, so the other lines can be
any text.

After that come slides, each after a blank line:

	* Title of slide (must have asterisk)

	Some Text
	Some More text

	- bullets
	- more bullets

	code x.go
	code y.go
	slide image.jpg

Blank lines are OK (not mandatory) after the title and after the text.
Text, bullets, and code are all optional; title is not.

Lines starting with # in column 1 are commentary.

The template uses the function "code" to inject program
source into the output by extracting code from files and
injecting them as HTML-escaped <pre> blocks.

The syntax is simple: 1, 2, or 3 space-separated arguments.
The first argument is always the file name.
The remainder are /-delimited patterns, decimal line numbers,
or $ to represent the end of the file.  No quotes and remember
the args are blank-seprated, so you might want to use . to
represent a blank.

Whole file:
code foo.go
One line (here the signature of main):
code foo.go /^func.main/
Block of text, determined by start and end (here the body of main):
code foo.go /^func.main/ /^}/

Patterns can be `/regular expression/`, a decimal number, or "$"
to signify the end of the file.

The template uses the function "image" to inject picture files.

The syntax is simple: 1 or 3 space-separated arguments.
The first argument is always the file name.
If there are more arguments, they are the height and width;
both must be present.

image images/betsy.jpg 100 200

*/
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

func Usage() {
	fmt.Fprintf(os.Stderr, "usage: makeslide file.slide > file.html\n")
	flag.PrintDefaults()
	os.Exit(2)
}

var templateFile = flag.String("template", "slide.tmpl", "file containing template definitions")
var html = flag.Bool("html", true, "print slides as HTML not plain text")
var start = flag.Int("start", 0, "first slide to print")

var funcs = template.FuncMap{
	"code": code,
	"image": image,
}

func main() {
	flag.Usage = Usage
	flag.Parse()
	if len(flag.Args()) != 1 {
		Usage()
	}

	// Read the input and build the slide structure.
	pres := parse(flag.Arg(0))
	if *start < len(pres.Slide) {
		pres.Slide = pres.Slide[*start:]
	}

	// Read and parse the input.
	name := *templateFile
	tmpl := template.New(name).Funcs(funcs)
	if *html {
		if _, err := tmpl.ParseFiles(name); err != nil {
			log.Fatal(err)
		}
	} else {
		if _, err := tmpl.Parse(textTemplate); err != nil {
			log.Fatal(err)
		}
	}

	// Execute the template.
	if err := tmpl.Execute(os.Stdout, pres); err != nil {
		log.Fatal(err)
	}
}

// Slide parsing.

type Pres struct {
	Title []string
	Info  []string
	Slide []Slide
}

type Slide struct {
	Number  int
	Title   string
	Text    []string
	Bullets []string
	Code    []Code
	Image    []Image
	BottomText    []string
}

type Code struct {
	File string
	Args []interface{}
}

type Image struct {
	File string
	Args []interface{}
}

type Lines struct {
	line int // 0 indexed, so has 1-indexed number of last line returned
	text []string
}

func readLines(name string) *Lines {
	return &Lines{
		0,
		strings.Split(contents(name), "\n"),
	}
}

func (l *Lines) next() (text string, ok bool) {
	for {
		if l.line >= len(l.text) {
			return "", false
		}
		text = l.text[l.line]
		l.line++
		// Lines starting with # are comments.
		if len(text) == 0 || text[0] != '#' {
			ok = true
			break
		}
	}
	return
}

func (l *Lines) back() {
	l.line--
}

func (l *Lines) nextNonEmpty() (text string, ok bool) {
	for {
		text, ok = l.next()
		if !ok {
			return
		}
		if len(text) > 0 {
			break
		}
	}
	return
}

var actionRE = regexp.MustCompile(`^(- |code|image|\*)`)

func parse(name string) *Pres {
	pres := new(Pres)
	lines := readLines(name)
	var ok bool
	// First non-empty line starts title.
	text, ok := lines.nextNonEmpty()
	if !ok {
		log.Fatalf("no title")
	}
	pres.Title = append(pres.Title, text)
	// Suck up rest of title.
	for {
		text, ok := lines.next()
		if !ok {
			log.Fatal("missing info")
		}
		if text == "" {
			break
		}
		pres.Title = append(pres.Title, text)
	}
	// Suck up rest of title slide.
	for {
		text, ok := lines.next()
		if !ok {
			log.Fatal("missing info")
		}
		if text == "" {
			break
		}
		pres.Info = append(pres.Info, text)
	}
	// Slides
	for i := 0; ; i++ {
		var slide Slide
		slide.Number = i
		// Next non-empty line is title.
		text, ok := lines.nextNonEmpty()
		for ok && text == "" {
			text, ok = lines.next()
		}
		if !ok {
			break
		}
		if !strings.HasPrefix(text, "* ") {
			log.Fatalf("%s:%d bad title %q", name, lines.line, text)
		}
		slide.Title = text[2:]
		// Next non-empty line is first line of text.
		text, ok = lines.nextNonEmpty()
		for ok && !actionRE.MatchString(text) {
			slide.Text = append(slide.Text, text)
			text, ok = lines.next()
		}
		for ok && strings.HasPrefix(text, "- ") {
			slide.Bullets = append(slide.Bullets, text[2:])
			text, ok = lines.next()
		}
		for ok && text == "" {
			text, ok = lines.next()
		}
		for ok && (strings.HasPrefix(text, "code ") || strings.HasPrefix(text , "image")) {
			args := strings.Fields(text)
			if len(args) < 2 {
				log.Fatalf("%s:%d bad action invocation syntax %q", name, lines.line, text)
			}
			switch args[0] {
			case "code":
				slide.Code = append(slide.Code, Code{args[1], parseArgs(name, lines.line, args[2:])})
				text, ok = lines.next()
			case "image":
				slide.Image = append(slide.Image, Image{args[1], parseArgs(name, lines.line, args[2:])})
				text, ok = lines.next()
			default:
				log.Fatalf("%s:%d unrecognized action %q", name, lines.line, text)
			}
		}
		for ok && !actionRE.MatchString(text) {
			slide.BottomText = append(slide.BottomText, text)
			text, ok = lines.next()
		}

		if strings.HasPrefix(text, "* ") {
			lines.back()
		}
		pres.Slide = append(pres.Slide, slide)
	}
	return pres
}

func parseArgs(name string, line int, args []string) (res []interface{}) {
	res = make([]interface{}, len(args))
	for i, v := range args {
		if len(v) == 0 {
			log.Fatalf("%s:%d bad code argument %q", name, line, v)
		}
		switch v[0] {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			n, err := strconv.Atoi(v)
			if err != nil {
				log.Fatalf("%s:%d bad code argument %q", name, line, v)
			}
			res[i] = n
		case '/':
			if len(v) < 2 || v[len(v)-1] != '/' {
				log.Fatalf("%s:%d bad code argument %q", name, line, v)
			}
			res[i] = v
		case '$':
			res[i] = "$"
		default:
			log.Fatalf("%s:%d bad code argument %q", name, line, v)
		}
	}
	return
}

// contents reads a file by name and returns its contents as a string.
func contents(name string) string {
	file, err := ioutil.ReadFile(name)
	if err != nil {
		log.Fatal(err)
	}
	return string(file)
}

// Template generation.

// format returns a textual representation of the arg, formatted according to its nature.
func format(arg interface{}) string {
	switch arg := arg.(type) {
	case int:
		return fmt.Sprintf("%d", arg)
	case string:
		if len(arg) > 2 && arg[0] == '/' && arg[len(arg)-1] == '/' {
			return fmt.Sprintf("%#q", arg)
		}
		return fmt.Sprintf("%q", arg)
	default:
		log.Fatalf("unrecognized argument: %v type %T", arg, arg)
	}
	return ""
}

func code(file string, arg []interface{}) (string, error) {
	text := contents(file)
	var command string
	switch len(arg) {
	case 0:
		// whole file.
		command = fmt.Sprintf("code %q", file)
	case 1:
		// one line, specified by line or regexp.
		command = fmt.Sprintf("code %q %s", file, format(arg[0]))
		text = oneLine(file, text, arg[0])
	case 2:
		// multiple lines, specified by line or regexp.
		command = fmt.Sprintf("code %q %s %s", file, format(arg[0]), format(arg[1]))
		text = multipleLines(file, text, arg[0], arg[1])
	default:
		return "", fmt.Errorf("incorrect code invocation: code %q %v", file, arg)
	}
	// Replace tabs by spaces, which work better in HTML.
	text = strings.Replace(text, "\t", "    ", -1)
	// Escape the program text for HTML.
	if *html {
		text = template.HTMLEscapeString(text)
	}
	// Include the command as a comment.
	if *html {
		text = fmt.Sprintf("<!--{{%s}}\n-->%s", command, text)
	} else {
		text = fmt.Sprintf("// %s\n%s", command, text)
	}
	return text, nil
}

func image(file string, arg []interface{}) (string, error) {
	args := ""
	switch len(arg) {
	case 0:
		// no size parameters
	case 2:
		args = fmt.Sprintf("height=%s width=%s", format(arg[0]), format(arg[1]))
	default:
		return "", fmt.Errorf("incorrect image invocation: code %q %v", file, arg)
	}
	return fmt.Sprintf(`<img src=%q %s>`, file, args), nil
}

// parseArg returns the integer or string value of the argument and tells which it is.
func parseArg(arg interface{}, file string, max int) (ival int, sval string, isInt bool) {
	switch n := arg.(type) {
	case int:
		if n <= 0 || n > max {
			log.Fatalf("%q:%d is out of range", file, n)
		}
		return n, "", true
	case string:
		return 0, n, false
	}
	log.Fatalf("unrecognized argument %v type %T", arg, arg)
	return
}

// oneLine returns the single line generated by a two-argument code invocation.
func oneLine(file, text string, arg interface{}) string {
	lines := strings.SplitAfter(contents(file), "\n")
	line, pattern, isInt := parseArg(arg, file, len(lines))
	if isInt {
		return lines[line-1]
	}
	return lines[match(file, 0, lines, pattern)-1]
}

// multipleLines returns the text generated by a three-argument code invocation.
func multipleLines(file, text string, arg1, arg2 interface{}) string {
	lines := strings.SplitAfter(contents(file), "\n")
	line1, pattern1, isInt1 := parseArg(arg1, file, len(lines))
	line2, pattern2, isInt2 := parseArg(arg2, file, len(lines))
	if !isInt1 {
		line1 = match(file, 0, lines, pattern1)
	}
	if !isInt2 {
		line2 = match(file, line1, lines, pattern2)
	} else if line2 < line1 {
		log.Fatalf("lines out of order for %q: %d %d", line1, line2)
	}
	return strings.Join(lines[line1-1:line2], "")
}

// match identifies the input line that matches the pattern in a code invocation.
// If start>0, match lines starting there rather than at the beginning.
// The return value is 1-indexed.
func match(file string, start int, lines []string, pattern string) int {
	// $ matches the end of the file.
	if pattern == "$" {
		if len(lines) == 0 {
			log.Fatalf("%q: empty file", file)
		}
		return len(lines)
	}
	// /regexp/ matches the line that matches the regexp.
	if len(pattern) > 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		re, err := regexp.Compile(pattern[1 : len(pattern)-1])
		if err != nil {
			log.Fatal(err)
		}
		for i := start; i < len(lines); i++ {
			if re.MatchString(lines[i]) {
				return i + 1
			}
		}
		log.Fatalf("%s: no match for %#q", file, pattern)
	}
	log.Fatalf("unrecognized pattern: %q", pattern)
	return 0
}

const textTemplate = `
{{range .Title}}{{.}}
{{end}}

{{range .Info}}{{.}}
{{end}}
{{range $s := .Slide}}---------------[{{$s.Number}}]

{{html $s.Title}}

{{range $s.Text}}{{.}}
{{end}}{{if $s.Bullets}}{{range $s.Bullets}}* {{.}}
{{end}}{{end}}
{{range $s.Code}}{{code .File .Args}}{{end}}{{end}}
{{range $s.BottomText}}{{.}}`
