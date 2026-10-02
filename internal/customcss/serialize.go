package customcss

import (
	"fmt"
	"strings"
)

// writer serializes component values. It escapes every character that could
// end the surrounding <style> element: '<', '>' and '&' never appear inside
// identifiers or strings, a '<' delimiter is always followed by a space, and
// a '>' delimiter never directly follows "--". Where two tokens would merge
// when written next to each other, an empty comment separates them.
type writer struct {
	b    strings.Builder
	last token
	any  bool
}

func serialize(ns []node) string {
	w := &writer{}
	w.nodes(ns)
	return w.b.String()
}

func (w *writer) nodes(ns []node) {
	for _, n := range ns {
		w.node(n)
	}
}

func (w *writer) node(n node) {
	t := n.tok
	switch t.kind {
	case tBadString, tBadURL, tCDO, tCDC:
		return // never emitted; the sanitizer rejects them first
	case tDelim:
		if t.value == "\\" {
			return
		}
	}
	if w.any && needsSeparator(w.last, t) {
		w.b.WriteString("/**/")
	}
	// '<' is only followed directly by '=' (media range syntax) or
	// whitespace, so "</" and "<!" can't form.
	if isDelim(w.last, "<") && !isDelim(t, "=") && t.kind != tWhitespace {
		w.b.WriteString(" ")
	}
	w.any = true
	w.last = t
	switch t.kind {
	case tIdent:
		w.b.WriteString(ident(t.value))
	case tFunction:
		w.b.WriteString(ident(t.value) + "(")
		w.nodes(n.children)
		w.b.WriteString(")")
		w.last = token{kind: tRParen}
	case tAtKeyword:
		w.b.WriteString("@" + ident(t.value))
	case tHash:
		w.b.WriteString("#" + name(t.value))
	case tString:
		w.b.WriteString(quote(t.value))
	case tURL:
		w.b.WriteString("url(" + quote(t.value) + ")")
	case tNumber:
		w.b.WriteString(t.num)
	case tPercentage:
		w.b.WriteString(t.num + "%")
	case tDimension:
		w.b.WriteString(t.num + unit(t.value))
	case tWhitespace:
		w.b.WriteString(" ")
	case tColon:
		w.b.WriteString(":")
	case tSemicolon:
		w.b.WriteString(";")
	case tComma:
		w.b.WriteString(",")
	case tLBrace, tLBracket, tLParen:
		open, close := "{", "}"
		if t.kind == tLBracket {
			open, close = "[", "]"
		} else if t.kind == tLParen {
			open, close = "(", ")"
		}
		w.b.WriteString(open)
		w.nodes(n.children)
		w.b.WriteString(close)
		w.last = token{kind: closer(t.kind)}
	case tRBrace, tRBracket, tRParen:
		// Stray closers only appear in rejected preludes.
	case tDelim:
		switch {
		case t.value == "<":
			w.b.WriteString("<")
		case t.value == ">" && strings.HasSuffix(w.b.String(), "--"):
			w.b.WriteString(" >")
		case t.value == "&" || t.value == ">" || t.value == "*" || t.value == "!" || t.value == "/" ||
			t.value == "." || t.value == "+" || t.value == "-" || t.value == "~" || t.value == "|" ||
			t.value == "#" || t.value == "@" || t.value == "=" || t.value == "^" || t.value == "$" ||
			t.value == "%" || t.value == "?":
			w.b.WriteString(t.value)
		default:
			// Any other delimiter is written as an escaped identifier
			// character so it can't form markup.
			w.b.WriteString(fmt.Sprintf("\\%x ", []rune(t.value)[0]))
		}
	}
}

func isDelim(t token, v string) bool { return t.kind == tDelim && t.value == v }

// needsSeparator follows the CSSOM table of token pairs that need a comment
// between them to keep their meaning.
func needsSeparator(a, b token) bool {
	identish := b.kind == tIdent || b.kind == tFunction || b.kind == tURL || b.kind == tBadURL
	numeric := b.kind == tNumber || b.kind == tPercentage || b.kind == tDimension
	switch {
	case a.kind == tIdent:
		return identish || isDelim(b, "-") || numeric || b.kind == tCDC || b.kind == tLParen
	case a.kind == tAtKeyword || a.kind == tHash || a.kind == tDimension:
		return identish || isDelim(b, "-") || numeric || b.kind == tCDC
	case isDelim(a, "#") || isDelim(a, "-"):
		return identish || isDelim(b, "-") || numeric || (isDelim(a, "-") && b.kind == tCDC)
	case a.kind == tNumber:
		return identish || numeric || isDelim(b, "%")
	case isDelim(a, "@"):
		return identish || isDelim(b, "-") || b.kind == tCDC
	case isDelim(a, ".") || isDelim(a, "+"):
		return numeric
	case isDelim(a, "/"):
		return isDelim(b, "*")
	}
	return false
}

func hexEscape(r rune) string { return fmt.Sprintf("\\%x ", r) }

func safeNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r >= 0x80
}

// ident serializes an identifier. Characters outside [A-Za-z0-9_-] and
// non-ASCII are hex-escaped, so '<' can never appear literally.
func ident(s string) string {
	rs := []rune(s)
	if len(rs) == 1 && rs[0] == '-' {
		return `\-`
	}
	var b strings.Builder
	for i, r := range rs {
		switch {
		case i == 0 && isDigit(r), i == 1 && isDigit(r) && rs[0] == '-':
			b.WriteString(hexEscape(r))
		case safeNameRune(r):
			b.WriteRune(r)
		default:
			b.WriteString(hexEscape(r))
		}
	}
	return b.String()
}

// name serializes a hash name, which may start with a digit.
func name(s string) string {
	var b strings.Builder
	for _, r := range s {
		if safeNameRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteString(hexEscape(r))
		}
	}
	return b.String()
}

// unit serializes a dimension unit. A leading e before a digit is escaped
// so "1" + "e3" doesn't read back as the number 1e3.
func unit(s string) string {
	if len(s) > 1 && (s[0] == 'e' || s[0] == 'E') &&
		(isDigit(rune(s[1])) || (s[1] == '+' || s[1] == '-') && len(s) > 2 && isDigit(rune(s[2]))) {
		return hexEscape(rune(s[0])) + ident(s[1:])
	}
	return ident(s)
}

// quote serializes a string in double quotes, hex-escaping quotes,
// backslashes, control characters and the HTML-significant < > &.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\' || r == '<' || r == '>' || r == '&' || r < 0x20 || r == 0x7f:
			b.WriteString(hexEscape(r))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
