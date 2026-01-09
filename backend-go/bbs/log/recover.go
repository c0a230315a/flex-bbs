package log

import "unicode/utf8"

func recoverUTF8FromLatin1(s string) (string, bool) {
	if s == "" {
		return s, false
	}

	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return s, false
		}
		b = append(b, byte(r))
	}
	if !utf8.Valid(b) {
		return s, false
	}
	recovered := string(b)
	if recovered == s {
		return s, false
	}
	return recovered, true
}
