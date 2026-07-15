package store

import (
	"testing"
)

func TestUpgradeAllFlipsActiveFormatSoNewProjectsAreBornV2(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeAllToV2(); err != nil {
		t.Fatal(err)
	}
	m, err := s.readStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m.ActiveFormat != StoreFormatV2 {
		t.Fatalf("ActiveFormat after upgrade --all = %q, want v2", m.ActiveFormat)
	}
	if f, _ := s.projectFormat("NEW"); f != StoreFormatV2 {
		t.Fatalf("birth format for a project with no entry = %q, want v2", f)
	}
}
