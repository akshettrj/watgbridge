package utils

func SubString(s string, start, end int) string {
	asRunes := []rune(s)

	if start > len(asRunes) {
		return ""
	}

	if end > len(asRunes) {
		end = len(asRunes)
	}

	return s[start:end]
}
