package codereview

import "atm/skills"

// Summary, Brief, and Guide are served from the embedded capability skill —
// the guide text has ONE home (skills/capability/codereview.md), so the CLI,
// the session context, and the docs cannot drift apart.
func (Cap) Summary() string { return skills.MustCapability(CapabilityName).Description }
func (Cap) Brief() string   { return skills.MustCapability(CapabilityName).Brief }
func (Cap) Guide() string   { return skills.MustCapability(CapabilityName).Body }
