package scrum

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the scrum capability. It implements capability.Flow: the interface is
// the kind declaration, so the registry can compute the unclaimed pool and the
// adapters can find its lanes without knowing anything else about scrum.
type Cap struct{}

// New builds the capability. The return type is Flow on purpose — the
// composition root reads the kind off the constructor.
func New() capability.Flow { return Cap{} }

func (Cap) Name() string { return CapabilityName }

func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }
func (Cap) EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(s, code, actor)
}

// ClaimExprs are the atoms that mean "scrum has decided about this task" —
// claimed or evicted. The registry negates them into the unclaimed pool.
func (Cap) ClaimExprs() []string { return []string{"scrum:*", "scrum-out:*"} }

// FinishLabel is scrum's declared finish socket. It is scrum-stage:done, not
// scrum:done: the scrum:* axis is the TYPE axis, so the finish value lives on
// the stage axis where it cannot collide with a type.
func (Cap) FinishLabel(code string) core.Label {
	return core.Label{Name: code + ":" + StageNamespace + ":" + StageDone}
}

// EvictLabel is the declared evict socket: a namespace descriptor, since any
// member of scrum-out:* means evicted-by-scrum.
func (Cap) EvictLabel(code string) core.Label {
	return core.Label{Name: code + ":" + OutNamespace + ":*"}
}

// Lanes names the three seeded lane boards.
func (Cap) Lanes(code string) capability.LaneSet {
	return capability.LaneSet{Inbox: BoardInbox(code), Pipeline: BoardPipeline(code), Out: BoardOut(code)}
}
