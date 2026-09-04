package core

// ApplyState is what applying one profile document does to the project
// record of the same name.
type ApplyState string

const (
	// ApplyCreate: no record of that name exists; apply creates it.
	ApplyCreate ApplyState = "create"
	// ApplyInSync: the record already carries the document's content. When
	// it sits at an older version of the same profile its origin is
	// restamped to the applied version — content unchanged, provenance
	// current.
	ApplyInSync ApplyState = "in-sync"
	// ApplyUpdate: the record is UNMODIFIED since the profile version it
	// came from (proven by comparing it against that version's document),
	// and the applied version changed the document. Safe to overwrite.
	ApplyUpdate ApplyState = "update"
	// ApplyConflict: the record differs and nothing proves the difference
	// is the profile's — it was edited locally, the project authored it,
	// it carries a pre-profile origin, or another profile owns it. Left
	// untouched unless forced.
	ApplyConflict ApplyState = "conflict"
)

// Record kinds a profile ships, in the order apply reports them.
const (
	ApplyKindPersona   = "persona"
	ApplyKindChecklist = "checklist"
	ApplyKindChannel   = "channel"
)

// ApplyItem is one document's fate.
type ApplyItem struct {
	Kind  string     `json:"kind"`
	Name  string     `json:"name"`
	State ApplyState `json:"state"`
	// Origin is the existing record's provenance ("" when creating).
	Origin string `json:"origin,omitempty"`
	// Reason explains a conflict, or names what an update changes.
	Reason string `json:"reason,omitempty"`
	// Restamp marks an in-sync record whose origin moves to the applied
	// version.
	Restamp bool `json:"restamp,omitempty"`
	// Forced marks a conflict that was overwritten because the caller
	// asked for it.
	Forced bool `json:"forced,omitempty"`
}

// ApplyCapability is one capability the profile presupposes and whether
// the project already had it enabled.
type ApplyCapability struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ApplyPlan is what applying a profile to a project would do (dry run) or
// did (apply). Items are ordered persona, checklist, channel, each
// name-sorted as the profile loads them.
type ApplyPlan struct {
	Ref          string            `json:"ref"`
	Capabilities []ApplyCapability `json:"capabilities,omitempty"`
	Items        []ApplyItem       `json:"items"`
}

// Conflicts lists the items left untouched.
func (p *ApplyPlan) Conflicts() []ApplyItem {
	var out []ApplyItem
	for _, it := range p.Items {
		if it.State == ApplyConflict && !it.Forced {
			out = append(out, it)
		}
	}
	return out
}

// Count tallies the items in one state (forced conflicts count as their
// own state, not as applied updates).
func (p *ApplyPlan) Count(state ApplyState) int {
	n := 0
	for _, it := range p.Items {
		if it.State == state {
			n++
		}
	}
	return n
}

// SetupStep is one piece of mechanical setup a profile still needs after
// apply: something only the project or this machine can answer, named with
// the exact command that answers it.
type SetupStep struct {
	// Kind: channel-endpoint | channel-missing | launcher.
	Kind    string `json:"kind"`
	Subject string `json:"subject,omitempty"`
	Detail  string `json:"detail"`
	Command string `json:"command,omitempty"`
}

const (
	SetupChannelEndpoint = "channel-endpoint"
	SetupChannelMissing  = "channel-missing"
	SetupLauncher        = "launcher"
)
