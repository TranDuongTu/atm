package scrum

import "testing"

func TestSkillGuideLoads(t *testing.T) {
	if (Cap{}).Summary() == "" {
		t.Fatal("summary must load from embedded capability skill")
	}
	if (Cap{}).Brief() == "" {
		t.Fatal("brief must load from embedded capability skill")
	}
	if (Cap{}).Guide() == "" {
		t.Fatal("guide must load from embedded capability skill")
	}
}
