package expr

import (
	"sort"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// The template delimiters and the prefix that stands in for a leading $.
const (
	open     = "{{"
	closing  = "}}"
	dollar   = "_dollar_"
	backTick = '`'
	escape   = '\\'
)

// Preprocess rewrites $-prefixed names outside string literals. Exported for
// tests, because the quote-awareness is the subtle part: expr-lang will not lex
// a $, so `$json` must become `_dollar_json`, while a `$` that belongs to the
// user's data — "total: $5" — must survive untouched.
func Preprocess(src string) string {
	var b strings.Builder
	b.Grow(len(src) + len(dollar))
	var quote byte // the quote we are inside, 0 when outside any literal
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			b.WriteByte(c)
			// Backticks are raw in expr-lang, so a backslash escapes nothing.
			if c == escape && quote != backTick && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', backTick:
			quote = c
			b.WriteByte(c)
		case '$':
			// Only a $ that starts an identifier is a name; "$5" is not.
			if i+1 < len(src) && isIdentStart(src[i+1]) {
				b.WriteString(dollar)
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// segment is one piece of a template: either literal text or an expression body.
type segment struct {
	text   string
	isExpr bool
}

func anyExpr(segs []segment) bool {
	for _, s := range segs {
		if s.isExpr {
			return true
		}
	}
	return false
}

// split cuts a parameter value into literal and expression segments. It walks
// the source rather than using a regexp so that braces and strings inside an
// expression — {{ {"a": {"b": 1}} }} — do not close the template early.
func split(src string) ([]segment, error) {
	var segs []segment
	for {
		i := strings.Index(src, open)
		if i < 0 {
			if src != "" {
				segs = append(segs, segment{text: src})
			}
			if len(segs) == 0 {
				segs = append(segs, segment{text: ""})
			}
			return segs, nil
		}
		if i > 0 {
			segs = append(segs, segment{text: src[:i]})
		}
		body := src[i+len(open):]
		end := findClose(body)
		if end < 0 {
			return nil, domain.Errorf(domain.ErrCodeExpression,
				"unclosed %q in %q", open, src)
		}
		segs = append(segs, segment{text: body[:end], isExpr: true})
		src = body[end+len(closing):]
	}
}

// findClose returns the index of the "}}" that closes the expression, tracking
// brace depth and string literals so an object literal or a "}" in a string
// does not terminate it. It returns -1 when the template is unclosed.
func findClose(s string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == escape && quote != backTick && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', backTick:
			quote = c
		case '{':
			depth++
		case '}':
			if depth == 0 && i+1 < len(s) && s[i+1] == '}' {
				return i
			}
			if depth > 0 {
				depth--
			}
		}
	}
	return -1
}

// sortedKeys gives map iteration a deterministic order, which matters wherever
// engine output is compared between two runs of the same graph.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
