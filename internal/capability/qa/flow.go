package qa

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the qa capability. It implements capability.Flow: the interface is
// the kind declaration.
type Cap struct{}

// New builds the capability. The return type is Flow on purpose — the
// composition root reads the kind off the constructor.
func New() capability.Flow { return Cap{} }

func (Cap) Name() string { return CapabilityName }

func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }
func (Cap) Exposed(code string) []core.Label    { return Exposed(code) }
func (Cap) EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(s, code, actor)
}

// ClaimExprs are the atoms that mean "qa has decided about this task".
func (Cap) ClaimExprs() []string { return []string{"qa:*", "qa-out:*"} }

// FinishLabel is qa's declared finish socket. Unlike scrum's, it lives on the
// claim axis itself: qa's claim axis IS its state, so a verified original
// stays claimed and stays visible in the pipeline lane.
func (Cap) FinishLabel(code string) core.Label {
	return core.Label{Name: code + ":" + ClaimNamespace + ":" + StateDone}
}

// EvictLabel is the declared evict socket: a namespace descriptor, since any
// member of qa-out:* means evicted-by-qa.
func (Cap) EvictLabel(code string) core.Label {
	return core.Label{Name: code + ":" + OutNamespace + ":*"}
}

// Lanes names the three seeded lane boards.
func (Cap) Lanes(code string) capability.LaneSet {
	return capability.LaneSet{Inbox: BoardInbox(code), Pipeline: BoardPipeline(code), Out: BoardOut(code)}
}
