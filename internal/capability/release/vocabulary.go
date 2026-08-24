package release

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// SanitizeVersion turns a human version into a label value: lower-cased, dots
// become dashes ("v1.2" -> "v1-2"), because the label grammar has no room for
// a dot. It refuses anything the grammar would reject rather than writing a
// label the store will bounce.
func SanitizeVersion(version string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(version))
	if v == "" {
		return "", fmt.Errorf("release requires --version")
	}
	v = strings.ReplaceAll(v, ".", "-")
	v = strings.ReplaceAll(v, "_", "-")
	if v == ValueShipped {
		return "", fmt.Errorf("version %q collides with the reserved value %q", version, ValueShipped)
	}
	if !versionValueRe.MatchString(v) {
		return "", fmt.Errorf("version %q does not fit the label grammar as %q (want lowercase letters, digits and dashes)", version, v)
	}
	return v, nil
}

// VersionLabel is the full stored label for a sanitized version value.
func VersionLabel(code, value string) string { return code + ":" + Namespace + ":" + value }

// ShippedLabel is the full stored label meaning "this change is shipped".
func ShippedLabel(code string) string { return VersionLabel(code, ValueShipped) }

// NamespaceLabel is the descriptor for the whole namespace.
func NamespaceLabel(code string) string { return code + ":" + Namespace + ":*" }

// vocabulary is the single literal list every contract method derives from.
// There are NO boards: a registry capability has no lanes. The namespace
// descriptor owns the version values `include` mints on demand, so they are
// never reported as unmanaged drift.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: NamespaceLabel(code), Description: "release membership; each value is one cut version (v1.2 -> release:v1-2), created on demand by `release include`"},
		{Name: ShippedLabel(code), Description: "release: this task's change is shipped (stamped on the container and every member by `release ship`)"},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the one ring entry a registry capability has: its namespace
// descriptor. It surfaces no lanes, because it has none.
func Exposed(code string) []core.Label {
	return []core.Label{vocabulary(code)[0]}
}

// EnsureVocabulary seeds the vocabulary idempotently. It returns no boards,
// because a registry capability seeds none.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	if err := s.LabelSeedBatch(vocabulary(code), actor); err != nil {
		return nil, err
	}
	return nil, nil
}
