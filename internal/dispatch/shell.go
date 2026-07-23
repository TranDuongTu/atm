package dispatch

import "strings"

// ShellCommand renders argv as one POSIX shell command string, each argument
// single-quoted; embedded single quotes use the '\” idiom.
func ShellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
