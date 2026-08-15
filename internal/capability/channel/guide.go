package channel

import "atm/skills"

func (Cap) Summary() string { return skills.MustCapability(CapabilityName).Description }
func (Cap) Brief() string   { return skills.MustCapability(CapabilityName).Brief }
func (Cap) Guide() string   { return skills.MustCapability(CapabilityName).Body }
