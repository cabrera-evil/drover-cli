package ansi_test

import (
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/ansi"
)

// AC36 (R48): the stripper keeps text and removes escape sequences.
func TestStrip(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain unchanged", "hello world\n", "hello world\n"},
		{"empty", "", ""},
		{"unicode unchanged", "● ready — ok ◌", "● ready — ok ◌"},
		{"sgr colour", "\x1b[31mred\x1b[0m text", "red text"},
		{"sgr truecolour", "\x1b[38;2;1;2;3mx\x1b[m", "x"},
		{"cursor moves", "a\x1b[2Ab\x1b[10;5Hc\x1b[2K\x1b[Jd", "abcd"},
		{"private mode", "\x1b[?25lhidden\x1b[?25h", "hidden"},
		{"osc title bel", "\x1b]0;my title\x07after", "after"},
		{"osc hyperlink st", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"multi line", "\x1b[32mone\x1b[0m\ntwo\n\x1b[1mthree\x1b[0m\n", "one\ntwo\nthree\n"},
		{"partial csi at end", "text\x1b[3", "text"},
		{"lone esc at end", "text\x1b", "text"},
		{"unterminated osc", "text\x1b]0;title", "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansi.Strip(tc.in); got != tc.want {
				t.Errorf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// AC36: stripping is idempotent and never leaves an ESC byte for well-formed input.
func TestStripIdempotent(t *testing.T) {
	inputs := []string{
		"plain", "\x1b[31mred\x1b[0m", "\x1b]0;t\x07x", "a\x1b[2K\rb", "\x1b[?25l\x1b[1;1Hz\x1b[0m\n",
		"text\x1b[3", "\x1b",
	}
	for _, in := range inputs {
		once := ansi.Strip(in)
		if twice := ansi.Strip(once); twice != once {
			t.Errorf("not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}
