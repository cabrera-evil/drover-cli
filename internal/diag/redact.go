package diag

import "strings"

const mask = "***"

// RedactCommand masks the value of every KEY=value env assignment in command position (the start of the command and
// after `&&`, `||`, `;`, `|`, `export` or `env`): `TOKEN=abc ./run` -> `TOKEN=*** ./run`. Arguments such as
// `-run=Foo` are untouched. Quoted values containing spaces are masked whole.
func RedactCommand(cmd string) string {
	var out strings.Builder
	atCommand := true
	for i := 0; i < len(cmd); {
		if isSpace(cmd[i]) {
			out.WriteByte(cmd[i])
			i++

			continue
		}
		end := tokenEnd(cmd, i)
		tok := cmd[i:end]
		i = end
		key, isAssign := assignment(tok)
		if isAssign && atCommand {
			out.WriteString(key + "=" + mask)

			continue
		}
		atCommand = tok == "export" || tok == "env" || isSeparator(tok)
		out.WriteString(tok)
	}

	return out.String()
}

func isSeparator(tok string) bool {
	switch tok {
	case "&&", "||", ";", "|":
		return true
	}

	return false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }

// tokenEnd returns the end of the whitespace-delimited token at start; quotes keep whitespace inside the token.
func tokenEnd(s string, start int) int {
	var quote byte
	for i := start; i < len(s); i++ {
		switch c := s[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case isSpace(c):
			return i
		}
	}

	return len(s)
}

// assignment reports whether tok is KEY=value with KEY matching [A-Za-z_][A-Za-z0-9_]*.
func assignment(tok string) (string, bool) {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return "", false
	}
	for i := range eq {
		c := tok[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return "", false
		}
	}

	return tok[:eq], true
}
