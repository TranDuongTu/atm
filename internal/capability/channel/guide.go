package channel

import "atm/skills"

func (Cap) Summary() string { return skills.MustCapability(CapabilityName).Description }
func (Cap) Guide() string   { return skills.MustCapability(CapabilityName).Body }
