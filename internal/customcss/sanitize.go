package customcss

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxInput is the largest custom CSS accepted, in bytes.
const MaxInput = 20 << 10

// MaxOutput caps the sanitized result, which can be longer than the input
// because of scoping prefixes and escapes.
const MaxOutput = 64 << 10

// Scope is the selector every rule is placed under. The public page's body
// carries the kork-page class; page chrome outside the body (the document
// root) cannot be selected.
const Scope = "body.kork-page"

// maxWarnings caps the warnings returned for one sanitize call.
const maxWarnings = 50

// UploadChecker reports whether an upload ID may be referenced, normally
// because the page owner uploaded it.
type UploadChecker func(id string) bool

// AnyUpload accepts every syntactically valid upload ID. Rendering uses it to
// re-sanitize CSS that was checked for ownership when it was saved.
func AnyUpload(string) bool { return true }

// Result is sanitized CSS and what was removed.
type Result struct {
	CSS      string
	Warnings []string
}

var uploadPath = regexp.MustCompile(`^/u/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// allowedFunctions are the functions allowed in declaration values. None of
// them loads a resource; url() is handled separately.
var allowedFunctions = map[string]bool{}

func init() {
	for _, f := range strings.Fields(`calc min max clamp round mod rem abs sign sin cos tan asin acos atan atan2 pow
		sqrt hypot log exp var env rgb rgba hsl hsla hwb lab lch oklab oklch color color-mix light-dark
		linear-gradient radial-gradient conic-gradient repeating-linear-gradient repeating-radial-gradient
		repeating-conic-gradient translate translatex translatey translatez translate3d rotate rotatex rotatey
		rotatez rotate3d scale scalex scaley scalez scale3d skew skewx skewy matrix matrix3d perspective
		cubic-bezier steps linear blur brightness contrast drop-shadow grayscale hue-rotate invert opacity
		saturate sepia attr counter counters minmax repeat fit-content inset circle ellipse polygon path rect xywh
		local format tech`) {
		allowedFunctions[f] = true
	}
}

// loadingFunctions can fetch a resource or run code and are never allowed,
// even in selectors and at-rule preludes.
var loadingFunctions = map[string]bool{
	"url": true, "src": true, "image": true, "image-set": true, "-webkit-image-set": true, "cross-fade": true,
	"-webkit-cross-fade": true, "element": true, "-moz-element": true, "expression": true, "paint": true,
}

// fontFaceDescriptors are the @font-face descriptors kept.
var fontFaceDescriptors = map[string]bool{
	"font-family": true, "src": true, "font-style": true, "font-weight": true, "font-stretch": true,
	"font-display": true, "unicode-range": true, "font-feature-settings": true, "font-variation-settings": true,
	"ascent-override": true, "descent-override": true, "line-gap-override": true, "size-adjust": true,
}

type sanitizer struct {
	ok       UploadChecker
	warnings []string
}

func (s *sanitizer) warn(line int, format string, args ...any) {
	if len(s.warnings) < maxWarnings {
		s.warnings = append(s.warnings, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, args...))
	}
}

// Sanitize keeps the safe subset of src, scoped to the page body:
//
//   - style rules, @media, @supports, @container, @keyframes and @font-face;
//     every other at-rule (@import, @charset, @namespace, @layer, @page, ...)
//     is removed;
//   - url() only for /u/<id> where ok(id) is true; no remote or data: URLs;
//   - functions from an allowlist (no expression(), image-set(), ...);
//   - no behavior or -moz-binding properties and no javascript: strings;
//   - nested rules inside a style rule are removed.
//
// Each selector is prefixed with Scope; a leading html, body or :root is
// replaced by it. Removed parts are listed in Result.Warnings with their line.
func Sanitize(src string, ok UploadChecker) Result {
	s := &sanitizer{ok: ok}
	if strings.TrimSpace(src) == "" {
		return Result{}
	}
	nodes, err := parseComponents(tokenize(src))
	if err != nil {
		return Result{Warnings: []string{"the CSS is nested too deeply; nothing was kept"}}
	}
	out := s.rules(parseRules(nodes), 0)
	lower := strings.ToLower(out)
	if strings.Contains(lower, "</") || strings.Contains(lower, "<!--") || strings.Contains(lower, "-->") {
		// The serializer never produces these; refuse rather than risk it.
		return Result{Warnings: []string{"the CSS could not be made safe; nothing was kept"}}
	}
	return Result{CSS: out, Warnings: s.warnings}
}

// rules sanitizes a list of rules at the top level or inside a conditional
// group rule.
func (s *sanitizer) rules(rs []rule, depth int) string {
	var b strings.Builder
	for _, r := range rs {
		if r.at == "" {
			b.WriteString(s.styleRule(r))
			continue
		}
		name := strings.ToLower(r.at)
		switch name {
		case "media", "supports", "container":
			if r.block == nil {
				s.warn(r.line, "@%s needs a block", name)
				continue
			}
			if depth >= 3 {
				s.warn(r.line, "@%s is nested too deeply", name)
				continue
			}
			if p := s.preludeProblem(r.prelude, false); p != "" {
				s.warn(r.line, "removed @%s: %s", name, p)
				continue
			}
			inner := s.rules(parseRules(r.block.children), depth+1)
			if inner == "" {
				continue
			}
			b.WriteString("@" + name + " " + serialize(trimSpace(r.prelude)) + "{" + inner + "}")
		case "keyframes", "-webkit-keyframes":
			b.WriteString(s.keyframes(name, r))
		case "font-face":
			b.WriteString(s.fontFace(r))
		default:
			s.warn(r.line, "@%s is not allowed", name)
		}
	}
	return b.String()
}

// styleRule sanitizes and scopes one style rule.
func (s *sanitizer) styleRule(r rule) string {
	if r.block == nil {
		s.warn(r.line, "removed text that isn't a complete rule")
		return ""
	}
	if p := s.preludeProblem(r.prelude, true); p != "" {
		s.warn(r.line, "removed a rule: %s", p)
		return ""
	}
	sel := scopeSelector(r.prelude)
	if sel == "" {
		s.warn(r.line, "removed a rule with an empty selector")
		return ""
	}
	body := s.decls(r.block.children, false)
	if body == "" {
		return ""
	}
	return sel + "{" + body + "}"
}

// selectorDelims are the delimiters that can appear in a selector.
var selectorDelims = map[string]bool{".": true, "*": true, ">": true, "+": true, "~": true, "|": true, "&": true,
	"=": true, "^": true, "$": true}

// conditionDelims are the delimiters allowed in @media, @supports and
// @container conditions: selector ones plus range syntax and ratios.
var conditionDelims = map[string]bool{".": true, "*": true, ">": true, "+": true, "~": true, "|": true, "&": true,
	"=": true, "^": true, "$": true, "<": true, "/": true, "-": true}

// preludeProblem checks a selector (selector true) or at-rule prelude. It may
// not load anything or contain broken tokens or stray delimiters.
func (s *sanitizer) preludeProblem(ns []node, selector bool) string {
	for _, n := range ns {
		switch n.tok.kind {
		case tDelim:
			if selector && !selectorDelims[n.tok.value] || !selector && !conditionDelims[n.tok.value] {
				return "invalid selector or condition"
			}
		case tURL, tBadURL:
			return "url() is not allowed here"
		case tBadString, tCDO, tCDC, tAtKeyword, tSemicolon, tRBrace, tRBracket, tRParen:
			return "invalid selector or condition"
		case tLBrace:
			return "unexpected block"
		case tString:
			if hasScript(n.tok.value) {
				return "javascript: is not allowed"
			}
		case tFunction:
			if loadingFunctions[strings.ToLower(n.tok.value)] {
				return strings.ToLower(n.tok.value) + "() is not allowed"
			}
		}
		if p := s.preludeProblem(n.children, selector); p != "" {
			return p
		}
	}
	return ""
}

// scopeSelector prefixes each selector in a list with Scope.
func scopeSelector(prelude []node) string {
	var parts []string
	var cur []node
	flush := func() {
		sel := trimSpace(cur)
		cur = nil
		if len(sel) == 0 {
			return
		}
		parts = append(parts, scopeOne(sel))
	}
	for _, n := range prelude {
		if n.tok.kind == tComma {
			flush()
			continue
		}
		cur = append(cur, n)
	}
	flush()
	return strings.Join(parts, ",")
}

func scopeOne(sel []node) string {
	isIdent := func(i int, v string) bool {
		return i < len(sel) && sel[i].tok.kind == tIdent && strings.EqualFold(sel[i].tok.value, v)
	}
	rest := -1
	switch {
	case isIdent(0, "html"):
		rest = 1
		// "html body" refers to the same element as Scope.
		if j := 1; j < len(sel) && sel[j].tok.kind == tWhitespace && isIdent(j+1, "body") {
			rest = j + 2
		}
	case isIdent(0, "body"):
		rest = 1
		// Already scoped (sanitized CSS is sanitized again when rendered).
		if len(sel) > 2 && isDelim(sel[1].tok, ".") && sel[2].tok.kind == tIdent && sel[2].tok.value == "kork-page" {
			rest = 3
		}
	case len(sel) > 1 && sel[0].tok.kind == tColon && isIdent(1, "root"):
		rest = 2
	}
	if rest < 0 {
		return Scope + " " + serialize(sel)
	}
	// The tail continues the compound selector ("body.dark") or starts with
	// a combinator or whitespace ("body > main").
	return Scope + serialize(sel[rest:])
}

// decls sanitizes the declarations of a style rule or keyframe.
func (s *sanitizer) decls(ns []node, fontFace bool) string {
	var out []string
	for _, d := range parseDecls(ns) {
		switch {
		case d.nested:
			s.warn(d.line, "nested rules are not supported; write them as separate rules")
			continue
		case d.invalid:
			s.warn(d.line, "removed something that isn't a property: value pair")
			continue
		}
		name := d.name
		lname := strings.ToLower(name)
		if !strings.HasPrefix(name, "--") {
			name = lname
		}
		if fontFace && !fontFaceDescriptors[lname] {
			s.warn(d.line, "removed @font-face descriptor %s", lname)
			continue
		}
		if strings.Contains(lname, "behavior") || strings.Contains(lname, "binding") {
			s.warn(d.line, "property %s is not allowed", lname)
			continue
		}
		if p := s.valueProblem(d.value); p != "" {
			s.warn(d.line, "removed %s: %s", lname, p)
			continue
		}
		out = append(out, ident(name)+":"+serialize(d.value))
	}
	return strings.Join(out, ";")
}

// valueProblem checks a declaration value.
func (s *sanitizer) valueProblem(ns []node) string {
	for _, n := range ns {
		switch n.tok.kind {
		case tURL:
			if p := s.urlProblem(n.tok.value); p != "" {
				return p
			}
			continue
		case tBadURL, tBadString, tCDO, tCDC, tAtKeyword, tRBrace, tRBracket, tRParen:
			return "invalid value"
		case tLBrace:
			return "blocks are not allowed in values"
		case tString:
			if hasScript(n.tok.value) {
				return "javascript: is not allowed"
			}
		case tFunction:
			fn := strings.ToLower(n.tok.value)
			if fn == "url" || fn == "src" {
				args := trimSpace(n.children)
				if len(args) != 1 || args[0].tok.kind != tString {
					return fn + "() needs a single quoted address"
				}
				if p := s.urlProblem(args[0].tok.value); p != "" {
					return p
				}
				continue
			}
			if !allowedFunctions[fn] {
				return fn + "() is not allowed"
			}
		}
		if p := s.valueProblem(n.children); p != "" {
			return p
		}
	}
	return ""
}

func (s *sanitizer) urlProblem(u string) string {
	m := uploadPath.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return "url() can only point at your own uploads (/u/...)"
	}
	if !s.ok(m[1]) {
		return "url() points at an upload that isn't yours"
	}
	return ""
}

// hasScript reports whether a string contains a javascript: or vbscript:
// URL, ignoring case, whitespace and control characters.
func hasScript(v string) bool {
	clean := strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToLower(v))
	return strings.Contains(clean, "javascript:") || strings.Contains(clean, "vbscript:")
}

// keyframes sanitizes @keyframes name { from {...} 50% {...} }.
func (s *sanitizer) keyframes(name string, r rule) string {
	pre := trimSpace(r.prelude)
	if r.block == nil || len(pre) != 1 || (pre[0].tok.kind != tIdent && pre[0].tok.kind != tString) {
		s.warn(r.line, "removed @%s: it needs a name and a block", name)
		return ""
	}
	var frames []string
	for _, f := range parseRules(r.block.children) {
		if f.at != "" || f.block == nil || !validKeyframeSelector(f.prelude) {
			s.warn(f.line, "removed an invalid keyframe")
			continue
		}
		body := s.decls(f.block.children, false)
		frames = append(frames, serialize(trimSpace(f.prelude))+"{"+body+"}")
	}
	if len(frames) == 0 {
		return ""
	}
	return "@" + name + " " + serialize(pre) + "{" + strings.Join(frames, "") + "}"
}

func validKeyframeSelector(ns []node) bool {
	if len(trimSpace(ns)) == 0 {
		return false
	}
	for _, n := range ns {
		switch n.tok.kind {
		case tWhitespace, tComma, tPercentage:
		case tIdent:
			v := strings.ToLower(n.tok.value)
			if v != "from" && v != "to" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// fontFace keeps an @font-face whose src loads only the owner's uploads.
func (s *sanitizer) fontFace(r rule) string {
	if r.block == nil || len(trimSpace(r.prelude)) > 0 {
		s.warn(r.line, "removed an invalid @font-face")
		return ""
	}
	hasSrc := false
	for _, d := range parseDecls(r.block.children) {
		if d.nested || d.invalid || !strings.EqualFold(d.name, "src") {
			continue
		}
		if p := s.valueProblem(d.value); p != "" {
			s.warn(d.line, "removed @font-face: %s", p)
			return ""
		}
		if !hasUpload(d.value) {
			s.warn(d.line, "removed @font-face: src must use url() with one of your uploaded fonts")
			return ""
		}
		hasSrc = true
	}
	if !hasSrc {
		s.warn(r.line, "removed @font-face without src")
		return ""
	}
	body := s.decls(r.block.children, true)
	if body == "" {
		return ""
	}
	return "@font-face{" + body + "}"
}

func hasUpload(ns []node) bool {
	for _, n := range ns {
		if n.tok.kind == tURL || n.tok.kind == tFunction && strings.EqualFold(n.tok.value, "url") {
			return true
		}
		if hasUpload(n.children) {
			return true
		}
	}
	return false
}
