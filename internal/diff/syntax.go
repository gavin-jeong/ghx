package diff

import (
	"path/filepath"
	"strings"
)

// Lightweight, extension-driven highlighting. A full syntax highlighter
// (chroma) would add megabytes and slow the render path; classifying comments
// and a small keyword set gives most of the visual value at near-zero cost.

// TokenKind classifies a span of a diff line for the renderer to style.
type TokenKind int

const (
	TokenPlain TokenKind = iota
	TokenComment
	TokenKeyword
)

// commentPrefix returns the line-comment marker for a path, or "" if the
// language has none (JSON) or is unknown.
func commentPrefix(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".c", ".h", ".cc", ".cpp", ".swift", ".kt":
		return "//"
	case ".yaml", ".yml", ".py", ".sh", ".bash", ".fish", ".rb", ".toml", ".tf", ".hcl", ".conf":
		return "#"
	case ".sql", ".lua":
		return "--"
	}
	return ""
}

// keywords returns the highlight set for a path's language.
func keywords(path string) map[string]bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return goKeywords
	case ".yaml", ".yml":
		return yamlKeywords
	case ".py":
		return pyKeywords
	}
	return nil
}

var goKeywords = map[string]bool{
	"func": true, "type": true, "const": true, "var": true, "if": true,
	"else": true, "for": true, "range": true, "return": true, "import": true,
	"package": true, "struct": true, "interface": true, "switch": true,
	"case": true, "default": true, "defer": true, "go": true, "chan": true,
	"map": true, "nil": true, "error": true,
}

var yamlKeywords = map[string]bool{
	"apiVersion": true, "kind": true, "metadata": true, "spec": true,
	"name": true, "namespace": true, "labels": true, "annotations": true,
	"containers": true, "image": true, "replicas": true, "selector": true,
}

var pyKeywords = map[string]bool{
	"def": true, "class": true, "import": true, "from": true, "return": true,
	"if": true, "elif": true, "else": true, "for": true, "while": true,
	"try": true, "except": true, "with": true, "as": true, "None": true,
}

// IsCommentLine reports whether a diff line's content is a whole-line comment
// for the given file. The renderer dims these.
func IsCommentLine(path, content string) bool {
	prefix := commentPrefix(path)
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(content), prefix)
}

// LeadingKeyword returns the first token of the line if it is a language
// keyword, plus its byte length in the original string (so the renderer can
// style just that span). Returns ok=false when the line doesn't start with one.
//
// For YAML the "keyword" is a mapping key, so a trailing ':' is tolerated.
func LeadingKeyword(path, content string) (kw string, end int, ok bool) {
	kws := keywords(path)
	if kws == nil {
		return "", 0, false
	}
	trimmed := strings.TrimLeft(content, " \t-")
	offset := len(content) - len(trimmed)
	// Take the first whitespace-delimited token.
	tokEnd := strings.IndexAny(trimmed, " \t(:{[")
	if tokEnd < 0 {
		tokEnd = len(trimmed)
	}
	tok := trimmed[:tokEnd]
	if tok == "" {
		return "", 0, false
	}
	if !kws[tok] {
		return "", 0, false
	}
	return tok, offset + tokEnd, true
}
