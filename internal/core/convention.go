package core

import (
	"fmt"
	"regexp"
	"time"
)

func RFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func Now() time.Time {
	return time.Now().UTC()
}

// TaskIDRe accepts both alias generations: v1 numeric ids ("ATM-0001") and
// v2 hash aliases ("ATM-7f3a2b" — MintTaskAlias mints "<CODE>-" + >=6
// lowercase hex, locally extended when taken). The alternation orders \d+
// first; an all-digit v2 hex extension therefore parses as numeric, which is
// harmless because the captured text is identical either way.
var TaskIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-(\d+|[0-9a-f]{6,})$`)

func ParseTaskID(id string) (code string, n int, ok bool) {
	m := TaskIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	return m[1], numericOrZero(m[2]), true
}

// numericOrZero parses an all-digit alias segment; v2 hex segments yield 0.
// n is v1 bookkeeping (RenderTaskID round-trips, sequential task-number
// recovery); v2 code paths key on the FULL alias string and must never depend on n.
func numericOrZero(seg string) int {
	v := 0
	for _, c := range seg {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c-'0')
	}
	return v
}

// CommentIDRe accepts v1 numeric comment ids ("ATM-0001-c0002") and v2 hash
// aliases ("ATM-7f3a2b-c9e1d" — MintCommentAlias mints "<task-alias>-c" +
// >=4 lowercase hex).
var CommentIDRe = regexp.MustCompile(`^([A-Z]{3,6})-(\d+|[0-9a-f]{6,})-c(\d+|[0-9a-f]{4,})$`)

func ParseCommentID(id string) (code string, taskN int, commentN int, ok bool) {
	m := CommentIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, false
	}
	return m[1], numericOrZero(m[2]), numericOrZero(m[3]), true
}

var personaNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func ValidatePersonaName(name string) error {
	if !personaNameRe.MatchString(name) {
		return fmt.Errorf("%w: invalid persona name %q (want ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$)", ErrUsage, name)
	}
	return nil
}
