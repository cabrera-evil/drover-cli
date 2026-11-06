// Package ansi removes terminal escape sequences from text.
package ansi

import "strings"

const (
	esc    = 0x1b
	bel    = 0x07
	escLen = 2 // ESC plus one byte
)

// Strip returns s without ANSI escape sequences: CSI (ESC [ ... final byte), OSC (ESC ] ... BEL or ESC \),
// and other two-byte ESC sequences. A partial or unterminated sequence at the end of s is dropped.
func Strip(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != esc {
			b.WriteByte(s[i])
			i++

			continue
		}
		i = skipEscape(s, i)
	}

	return b.String()
}

// skipEscape returns the index just past the escape sequence that starts at s[i] (which is ESC).
func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		for j := i + escLen; j < len(s); j++ { // parameter and intermediate bytes are 0x20-0x3F, final is 0x40-0x7E
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
	case ']':
		for j := i + escLen; j < len(s); j++ {
			if s[j] == bel {
				return j + 1
			}
			if s[j] == esc && j+1 < len(s) && s[j+1] == '\\' {
				return j + escLen
			}
		}
	default:
		return i + escLen
	}

	return len(s)
}
