package store

import (
	"reflect"
	"testing"
)

func TestProjectCapabilityEnableDisable(t *testing.T) {
	s := newTestStore(t) // reuse this package's existing test-store constructor (see project_test.go)
	actor := "admin@cli:unset"
	if _, err := s.CreateProject("PCA", "cap demo", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableProjectCapability("PCA", "workflow", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableProjectCapability("PCA", "contextmap", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableProjectCapability("PCA", "contextmap", actor); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("PCA")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Capabilities, []string{"workflow"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
}

func TestProjectWithoutCapabilityEventsReadsNil(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PLG", "legacy-like", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("PLG")
	if err != nil {
		t.Fatal(err)
	}
	if p.Capabilities != nil {
		t.Fatalf("Capabilities = %v, want nil (no capability events recorded)", p.Capabilities)
	}
}
