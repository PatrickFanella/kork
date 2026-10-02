// Package customcss sanitizes the custom CSS that Pro owners add to their
// pages. It tokenizes the input with the CSS Syntax Level 3 algorithm,
// parses it into rules, keeps only an allowlisted subset, scopes every
// selector to the page body and serializes the result from tokens. Nothing
// from the input reaches the output without passing through the tokenizer
// and serializer, so comments, escapes and string tricks cannot smuggle
// markup out of the page's <style> element.
package customcss

import (
	"strings"
	"unicode/utf8"
)

type kind int

const (
	tIdent kind = iota
	tFunction
	tAtKeyword
	tHash
	tString
	tBadString
	tURL
	tBadURL
	tDelim
	tNumber
	tPercentage
	tDimension
	tWhitespace
	tCDO
	tCDC
	tColon
	tSemicolon
	tComma
	tLBracket
	tRBracket
	tLParen
	tRParen
	tLBrace
	tRBrace
	tEOF
)

// token is one CSS token. value holds the decoded name, string or URL, the
// delimiter character, or a dimension's unit; num holds the source text of
// a number.
type token struct {
	kind  kind
	value string
	num   string
	line  int
}

type tokenizer struct {
	src  []rune
	pos  int
	line int
}

const eof = rune(-1)

// preprocess applies the input stream rules: CR, CRLF and FF become LF, NUL
// becomes U+FFFD, and invalid UTF-8 is replaced.
func preprocess(s string) []rune {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.NewReplacer("\r", "\n", "\f", "\n", "\x00", "�").Replace(s)
	return []rune(s)
}

func tokenize(s string) []token {
	t := &tokenizer{src: preprocess(s), line: 1}
	var out []token
	for {
		tok := t.next()
		if tok.kind == tEOF {
			return out
		}
		out = append(out, tok)
	}
}

func (t *tokenizer) peek(n int) rune {
	if t.pos+n < len(t.src) {
		return t.src[t.pos+n]
	}
	return eof
}

func (t *tokenizer) consume() rune {
	if t.pos >= len(t.src) {
		return eof
	}
	r := t.src[t.pos]
	t.pos++
	if r == '\n' {
		t.line++
	}
	return r
}

func isDigit(r rune) bool      { return r >= '0' && r <= '9' }
func isHex(r rune) bool        { return isDigit(r) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' }
func isWhitespace(r rune) bool { return r == '\n' || r == '\t' || r == ' ' }
func isIdentStart(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r >= 0x80
}
func isIdentChar(r rune) bool { return isIdentStart(r) || isDigit(r) || r == '-' }
func isNonPrintable(r rune) bool {
	return r >= 0 && r <= 8 || r == 0x0b || r >= 0x0e && r <= 0x1f || r == 0x7f
}

func validEscape(a, b rune) bool { return a == '\\' && b != '\n' }

func startsIdent(a, b, c rune) bool {
	switch {
	case a == '-':
		return isIdentStart(b) || b == '-' || validEscape(b, c)
	case isIdentStart(a):
		return true
	case a == '\\':
		return validEscape(a, b)
	}
	return false
}

func startsNumber(a, b, c rune) bool {
	switch {
	case a == '+' || a == '-':
		return isDigit(b) || b == '.' && isDigit(c)
	case a == '.':
		return isDigit(b)
	}
	return isDigit(a)
}

// skipComments consumes any comments; an unterminated one runs to the end.
func (t *tokenizer) skipComments() {
	for t.peek(0) == '/' && t.peek(1) == '*' {
		t.consume()
		t.consume()
		for {
			r := t.consume()
			if r == eof {
				return
			}
			if r == '*' && t.peek(0) == '/' {
				t.consume()
				break
			}
		}
	}
}

func (t *tokenizer) next() token {
	t.skipComments()
	line := t.line
	tok := t.consumeToken()
	tok.line = line
	return tok
}

func (t *tokenizer) consumeToken() token {
	r := t.consume()
	switch {
	case r == eof:
		return token{kind: tEOF}
	case isWhitespace(r):
		for isWhitespace(t.peek(0)) {
			t.consume()
		}
		return token{kind: tWhitespace}
	case r == '"' || r == '\'':
		return t.consumeString(r)
	case r == '#':
		if isIdentChar(t.peek(0)) || validEscape(t.peek(0), t.peek(1)) {
			return token{kind: tHash, value: t.consumeName()}
		}
		return token{kind: tDelim, value: "#"}
	case r == '(':
		return token{kind: tLParen}
	case r == ')':
		return token{kind: tRParen}
	case r == '[':
		return token{kind: tLBracket}
	case r == ']':
		return token{kind: tRBracket}
	case r == '{':
		return token{kind: tLBrace}
	case r == '}':
		return token{kind: tRBrace}
	case r == ',':
		return token{kind: tComma}
	case r == ':':
		return token{kind: tColon}
	case r == ';':
		return token{kind: tSemicolon}
	case r == '+' || r == '.':
		if startsNumber(r, t.peek(0), t.peek(1)) {
			t.pos--
			return t.consumeNumeric()
		}
		return token{kind: tDelim, value: string(r)}
	case r == '-':
		if startsNumber(r, t.peek(0), t.peek(1)) {
			t.pos--
			return t.consumeNumeric()
		}
		if t.peek(0) == '-' && t.peek(1) == '>' {
			t.consume()
			t.consume()
			return token{kind: tCDC}
		}
		if startsIdent(r, t.peek(0), t.peek(1)) {
			t.pos--
			return t.consumeIdentLike()
		}
		return token{kind: tDelim, value: "-"}
	case r == '<':
		if t.peek(0) == '!' && t.peek(1) == '-' && t.peek(2) == '-' {
			t.consume()
			t.consume()
			t.consume()
			return token{kind: tCDO}
		}
		return token{kind: tDelim, value: "<"}
	case r == '@':
		if startsIdent(t.peek(0), t.peek(1), t.peek(2)) {
			return token{kind: tAtKeyword, value: t.consumeName()}
		}
		return token{kind: tDelim, value: "@"}
	case r == '\\':
		if validEscape(r, t.peek(0)) {
			t.pos--
			return t.consumeIdentLike()
		}
		return token{kind: tDelim, value: "\\"}
	case isDigit(r):
		t.pos--
		return t.consumeNumeric()
	case isIdentStart(r):
		t.pos--
		return t.consumeIdentLike()
	}
	return token{kind: tDelim, value: string(r)}
}

// consumeEscape decodes an escape after its backslash.
func (t *tokenizer) consumeEscape() rune {
	r := t.consume()
	switch {
	case r == eof:
		return utf8.RuneError
	case isHex(r):
		v := hexVal(r)
		for i := 0; i < 5 && isHex(t.peek(0)); i++ {
			v = v*16 + hexVal(t.consume())
		}
		if isWhitespace(t.peek(0)) {
			t.consume()
		}
		if v == 0 || v >= 0xD800 && v <= 0xDFFF || v > 0x10FFFF {
			return utf8.RuneError
		}
		return rune(v)
	}
	return r
}

func hexVal(r rune) int {
	switch {
	case isDigit(r):
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	}
	return int(r-'A') + 10
}

func (t *tokenizer) consumeName() string {
	var b strings.Builder
	for {
		r := t.peek(0)
		switch {
		case isIdentChar(r):
			b.WriteRune(t.consume())
		case validEscape(r, t.peek(1)):
			t.consume()
			b.WriteRune(t.consumeEscape())
		default:
			return b.String()
		}
	}
}

func (t *tokenizer) consumeNumber() string {
	var b strings.Builder
	if r := t.peek(0); r == '+' || r == '-' {
		b.WriteRune(t.consume())
	}
	for isDigit(t.peek(0)) {
		b.WriteRune(t.consume())
	}
	if t.peek(0) == '.' && isDigit(t.peek(1)) {
		b.WriteRune(t.consume())
		for isDigit(t.peek(0)) {
			b.WriteRune(t.consume())
		}
	}
	if e := t.peek(0); e == 'e' || e == 'E' {
		s := t.peek(1)
		if isDigit(s) || (s == '+' || s == '-') && isDigit(t.peek(2)) {
			b.WriteRune(t.consume())
			if s == '+' || s == '-' {
				b.WriteRune(t.consume())
			}
			for isDigit(t.peek(0)) {
				b.WriteRune(t.consume())
			}
		}
	}
	return b.String()
}

func (t *tokenizer) consumeNumeric() token {
	num := t.consumeNumber()
	if startsIdent(t.peek(0), t.peek(1), t.peek(2)) {
		return token{kind: tDimension, num: num, value: t.consumeName()}
	}
	if t.peek(0) == '%' {
		t.consume()
		return token{kind: tPercentage, num: num}
	}
	return token{kind: tNumber, num: num}
}

func (t *tokenizer) consumeIdentLike() token {
	name := t.consumeName()
	if strings.EqualFold(name, "url") && t.peek(0) == '(' {
		t.consume()
		for isWhitespace(t.peek(0)) && isWhitespace(t.peek(1)) {
			t.consume()
		}
		a, b := t.peek(0), t.peek(1)
		if a == '"' || a == '\'' || isWhitespace(a) && (b == '"' || b == '\'') {
			return token{kind: tFunction, value: name}
		}
		return t.consumeURL()
	}
	if t.peek(0) == '(' {
		t.consume()
		return token{kind: tFunction, value: name}
	}
	return token{kind: tIdent, value: name}
}

func (t *tokenizer) consumeURL() token {
	var b strings.Builder
	for isWhitespace(t.peek(0)) {
		t.consume()
	}
	for {
		r := t.consume()
		switch {
		case r == ')' || r == eof:
			return token{kind: tURL, value: b.String()}
		case isWhitespace(r):
			for isWhitespace(t.peek(0)) {
				t.consume()
			}
			if p := t.peek(0); p == ')' || p == eof {
				t.consume()
				return token{kind: tURL, value: b.String()}
			}
			t.consumeBadURL()
			return token{kind: tBadURL}
		case r == '"' || r == '\'' || r == '(' || isNonPrintable(r):
			t.consumeBadURL()
			return token{kind: tBadURL}
		case r == '\\':
			if validEscape(r, t.peek(0)) {
				b.WriteRune(t.consumeEscape())
				continue
			}
			t.consumeBadURL()
			return token{kind: tBadURL}
		default:
			b.WriteRune(r)
		}
	}
}

func (t *tokenizer) consumeBadURL() {
	for {
		r := t.consume()
		switch {
		case r == ')' || r == eof:
			return
		case validEscape(r, t.peek(0)):
			t.consumeEscape()
		}
	}
}

func (t *tokenizer) consumeString(end rune) token {
	var b strings.Builder
	for {
		r := t.peek(0)
		switch {
		case r == eof:
			return token{kind: tString, value: b.String()}
		case r == end:
			t.consume()
			return token{kind: tString, value: b.String()}
		case r == '\n':
			return token{kind: tBadString}
		case r == '\\':
			t.consume()
			switch t.peek(0) {
			case eof:
			case '\n':
				t.consume()
			default:
				b.WriteRune(t.consumeEscape())
			}
		default:
			b.WriteRune(t.consume())
		}
	}
}
