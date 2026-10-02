package customcss

import "errors"

// node is a component value: a preserved token, a function with its
// arguments, or a {}, [] or () block with its contents.
type node struct {
	tok      token
	children []node
}

func (n node) isBlock(k kind) bool {
	return n.tok.kind == k && (k == tLBrace || k == tLBracket || k == tLParen)
}

// maxDepth bounds nesting of blocks and functions.
const maxDepth = 32

var errTooDeep = errors.New("too deeply nested")

type parser struct {
	toks []token
	pos  int
}

// parseComponents turns the token stream into component values.
func parseComponents(toks []token) ([]node, error) {
	p := &parser{toks: toks}
	var out []node
	for p.pos < len(p.toks) {
		n, err := p.component(0)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func closer(k kind) kind {
	switch k {
	case tLBrace:
		return tRBrace
	case tLBracket:
		return tRBracket
	}
	return tRParen
}

func (p *parser) component(depth int) (node, error) {
	t := p.toks[p.pos]
	p.pos++
	switch t.kind {
	case tLBrace, tLBracket, tLParen, tFunction:
		if depth >= maxDepth {
			return node{}, errTooDeep
		}
		end := tRParen
		if t.kind != tFunction {
			end = closer(t.kind)
		}
		n := node{tok: t}
		for p.pos < len(p.toks) {
			if p.toks[p.pos].kind == end {
				p.pos++
				return n, nil
			}
			c, err := p.component(depth + 1)
			if err != nil {
				return node{}, err
			}
			n.children = append(n.children, c)
		}
		return n, nil // unclosed at EOF: closed implicitly
	}
	return node{tok: t}, nil
}

// rule is an at-rule or a qualified (style) rule.
type rule struct {
	at      string // at-rule name as written; empty for qualified rules
	line    int
	prelude []node
	block   *node // the {} block; nil for at-rules ended by ';'
}

// parseRules reads a list of rules. Whitespace, CDO and CDC between rules
// are skipped. A qualified rule that reaches the end without a block has a
// nil block; the sanitizer drops it.
func parseRules(nodes []node) []rule {
	var out []rule
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		switch n.tok.kind {
		case tWhitespace, tCDO, tCDC, tSemicolon:
			continue
		}
		r := rule{line: n.tok.line}
		if n.tok.kind == tAtKeyword {
			r.at = n.tok.value
			i++
		}
		for ; i < len(nodes); i++ {
			c := nodes[i]
			if r.at != "" && c.tok.kind == tSemicolon {
				break
			}
			if c.isBlock(tLBrace) {
				b := c
				r.block = &b
				break
			}
			r.prelude = append(r.prelude, c)
		}
		out = append(out, r)
	}
	return out
}

// decl is one declaration, or a nested rule found in a declaration list.
type decl struct {
	name  string
	line  int
	value []node
	// nested is set for a nested rule or at-rule, which is dropped.
	nested bool
	// invalid is set for anything else that isn't name: value.
	invalid bool
}

// parseDecls splits a style block's contents into declarations.
func parseDecls(nodes []node) []decl {
	var out []decl
	i := 0
	for i < len(nodes) {
		for i < len(nodes) && (nodes[i].tok.kind == tWhitespace || nodes[i].tok.kind == tSemicolon) {
			i++
		}
		if i >= len(nodes) {
			break
		}
		start := i
		nested := false
		for i < len(nodes) && nodes[i].tok.kind != tSemicolon {
			if nodes[i].isBlock(tLBrace) {
				// A {} block ends a nested rule (CSS Nesting).
				nested = true
				i++
				break
			}
			i++
		}
		seg := nodes[start:i]
		d := decl{line: seg[0].tok.line, nested: nested}
		if !nested {
			d = splitDecl(seg)
		}
		out = append(out, d)
	}
	return out
}

func splitDecl(seg []node) decl {
	d := decl{line: seg[0].tok.line}
	if seg[0].tok.kind != tIdent {
		d.invalid = true
		return d
	}
	d.name = seg[0].tok.value
	j := 1
	for j < len(seg) && seg[j].tok.kind == tWhitespace {
		j++
	}
	if j >= len(seg) || seg[j].tok.kind != tColon {
		d.invalid = true
		return d
	}
	d.value = trimSpace(seg[j+1:])
	if len(d.value) == 0 {
		d.invalid = true
	}
	return d
}

func trimSpace(ns []node) []node {
	for len(ns) > 0 && ns[0].tok.kind == tWhitespace {
		ns = ns[1:]
	}
	for len(ns) > 0 && ns[len(ns)-1].tok.kind == tWhitespace {
		ns = ns[:len(ns)-1]
	}
	return ns
}
