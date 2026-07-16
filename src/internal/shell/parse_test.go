package shell

import (
	"reflect"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple command",
			input:    "echo hello",
			expected: []string{"echo", "hello"},
		},
		{
			name:     "command with multiple args",
			input:    "git commit -m message",
			expected: []string{"git", "commit", "-m", "message"},
		},
		{
			name:     "command with quoted argument",
			input:    `python -c "print(123)"`,
			expected: []string{"python", "-c", "print(123)"},
		},
		{
			name:     "command with quoted argument containing spaces",
			input:    `echo "hello world"`,
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "command with single quotes (treated as regular chars)",
			input:    `echo 'hello world'`,
			expected: []string{"echo", "'hello", "world'"},
		},
		{
			name:     "command with escaped double-quotes",
			input:    `echo "hello \"world\""`,
			expected: []string{"echo", `hello "world"`},
		},
		{
			name:     "path with spaces (quoted)",
			input:    `"C:\Program Files\app.exe" --version`,
			expected: []string{`C:\Program Files\app.exe`, "--version"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "single word",
			input:    "dir",
			expected: []string{"dir"},
		},
		{
			name:     "npm install with flags",
			input:    "npm install --save-dev typescript",
			expected: []string{"npm", "install", "--save-dev", "typescript"},
		},
		{
			name:     "python with file path argument",
			input:    "python script.py arg1 arg2",
			expected: []string{"python", "script.py", "arg1", "arg2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommand(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseCommand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNeedsShell(t *testing.T) {
	// These should NOT need shell (no metacharacters outside quotes)
	directCommands := []string{
		"echo hello",
		"python -c \"print(123)\"",
		"git status",
		"npm install",
		"dir",
		"where python",
		"python script.py",
		// Quote-aware: metacharacters inside quotes are literal
		`echo "hello|world"`,
		`echo "price $5"`,
		`python -c "a = 1 > 2; print(a)"`,
	}
	for _, cmd := range directCommands {
		if needsShell(cmd) {
			t.Errorf("needsShell(%q) = true, want false (should be direct execution)", cmd)
		}
	}

	// These SHOULD need shell (metacharacters outside quotes)
	shellCommands := []string{
		"dir | findstr foo",
		"echo hello > file.txt",
		"npm install && npm test",
		"echo %PATH%",
		"dir *.go",
		"type file.txt | findstr pattern > result.txt",
		// Edge: unclosed quotes are still literal up to the quote
		`echo "hello | still_quoted"`,
	}
	for _, cmd := range shellCommands {
		// The last one should be direct because | is inside quotes
		if cmd == `echo "hello | still_quoted"` {
			if needsShell(cmd) {
				t.Errorf("needsShell(%q) = true, want false (| inside double quotes)", cmd)
			}
			continue
		}
		if !needsShell(cmd) {
			t.Errorf("needsShell(%q) = false, want true (should use shell)", cmd)
		}
	}
}
