package utils

import "strings"

var (
	markdownEscaper = strings.NewReplacer(
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"{", "\\{",
		"}", "\\}",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
	)
)

func MarkdownEscapeString(s string) string {
	return markdownEscaper.Replace(s)
}
