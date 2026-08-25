package release

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the release capability.
type Cap struct{}

// New builds the capability. The return type is Capability, NOT Flow, and that
// is the whole kind declaration: nothing here implements the flow contract, so
// the registry, the switcher, and the manager's triage loop all skip it
// without needing to be told.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return CapabilityName }

func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }
func (Cap) EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(s, code, actor)
}
