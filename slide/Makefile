# Copyright 2009 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

include $(GOROOT)/src/Make.inc

TARG=makeslide
GOFILES=\
	makeslide.go\

lex.html: lex.slide makeslide slide.tmpl lex/lex1.go lex/r59-lex.go lex/snippets
	makeslide lex.slide > lex.html


include $(GOROOT)/src/Make.cmd
