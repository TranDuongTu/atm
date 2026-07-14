package store

import (
	"testing"

	"atm/internal/eventsource"
)

func TestAppendV2LockedParentsSecondLocalWriteOnFirst(t *testing.T) {
	s := testStore(t)
	if err := s.setProjectFormat("ATM", StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	var firstID string
	if err := s.WithLock("ATM", func() error {
		ev, err := s.appendV2Locked("ATM", V2Draft{
			Actor:   "admin@cli:unset",
			Action:  "project.created",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"alias": "ATM", "name": "x"},
		})
		if err != nil {
			return err
		}
		firstID = ev.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock("ATM", func() error {
		ev, err := s.appendV2Locked("ATM", V2Draft{
			Actor:   "admin@cli:unset",
			Action:  "project.name-changed",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"name": "y"},
		})
		if err != nil {
			return err
		}
		if len(ev.Parents) != 1 || ev.Parents[0] != firstID {
			t.Fatalf("parents = %#v, want [%s]", ev.Parents, firstID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
