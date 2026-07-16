package shell

import (
	"strings"
	"unicode"
)

// parseCommand splits a command string into program + arguments using
// Windows command-line parsing rules (compatible with CommandLineToArgvW).
//
// This is used for direct execution — no shell involved. The resulting
// parts are passed directly to os/exec as separate arguments, eliminating
// all quoting and injection issues.
//
// Parsing rules (matching Windows conventions):
//   - Arguments are separated by whitespace (outside quotes)
//   - Double-quoted strings are treated as a single argument (quotes removed)
//   - Backslashes before a double-quote are interpreted literally (2N→N)
//   - Backslashes not before a quote are kept as-is
func parseCommand(cmdline string) []string {
	var args []string
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return nil
	}

	i := 0
	n := len(cmdline)
	for i < n {
		// Skip whitespace.
		for i < n && isShellSpace(cmdline[i]) {
			i++
		}
		if i >= n {
			break
		}

		var arg strings.Builder
		inQuotes := false
		for i < n {
			ch := cmdline[i]
			if inQuotes {
				if ch == '"' {
					// Look ahead: are there more double-quotes? ("" → literal quote)
					if i+1 < n && cmdline[i+1] == '"' {
						arg.WriteByte('"')
						i += 2
					} else {
						inQuotes = false
						i++
					}
				} else if ch == '\\' && i+1 < n && cmdline[i+1] == '"' {
					// Backslash before a closing quote.
					backslashCount := 0
					for i < n && cmdline[i] == '\\' {
						backslashCount++
						i++
					}
					if i < n && cmdline[i] == '"' {
						// Each pair of backslashes → one literal backslash
						for j := 0; j < backslashCount/2; j++ {
							arg.WriteByte('\\')
						}
						if backslashCount%2 == 0 {
							// Even number: backslashes end, quote closes the argument.
							inQuotes = false
						} else {
							// Odd number: last backslash escapes the quote → literal quote.
							arg.WriteByte('"')
						}
						i++ // skip the quote
					} else {
						// Backslashes not followed by a quote — keep them all.
						for j := 0; j < backslashCount; j++ {
							arg.WriteByte('\\')
						}
					}
				} else {
					arg.WriteByte(ch)
					i++
				}
			} else {
				// Not in quotes.
				if isShellSpace(ch) {
					break
				}
				if ch == '"' {
					inQuotes = true
					i++
				} else {
					arg.WriteByte(ch)
					i++
				}
			}
		}
		args = append(args, arg.String())
	}
	return args
}

// escapeForCmd escapes a command string for use with cmd.exe /s /c.
// The /s flag means cmd.exe strips the first and last quote from the
// command line; everything inside is the command to run.
//
// Key rule: inside a quoted cmd /c string, double-quote characters
// that need to be literal must be escaped as \".
func escapeForCmd(s string) string {
	// The command goes inside double quotes for cmd /s /c "command".
	// Internal double quotes need to be backslash-escaped.
	var b strings.Builder
	for _, ch := range s {
		if ch == '"' {
			b.WriteString(`\"`)
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func isShellSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || isUnicodeSpace(rune(ch))
}

func isUnicodeSpace(r rune) bool {
	return unicode.IsSpace(r) && r != '\n' && r != '\r'
}
