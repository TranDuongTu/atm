package workflow

import "atm/skills"

// Summary is the capability's one-line description for enumeration surfaces.
// Single source: the skills file's frontmatter description.
func (Cap) Summary() string { return skills.MustCapability("workflow").Description }

// Guide is the capability's full agent-facing semantics; `atm capability
// workflow guide` prints it verbatim from the skills file.
func (Cap) Guide() string { return skills.MustCapability("workflow").Body }
