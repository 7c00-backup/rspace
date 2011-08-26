# Copyright 2009 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

include $(GOROOT)/src/Make.inc

TARG=makeslide
GOFILES=\
	makeslide.go\

lex.html: lex.slide makeslide slide.tmpl
	makeslide lex.slide > lex.html


include $(GOROOT)/src/Make.cmd
