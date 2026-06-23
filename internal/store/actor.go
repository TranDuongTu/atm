package store

import (
	"fmt"
	"sort"
	"time"
)

// Actor is an agent or human recorded in actors.json. Actors are registered lazily
// on first mutation; the registry is informational provenance, not authn (local-trust).
type Actor struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
}

type actorsFile struct {
	Actors []Actor `json:"actors"`
}

// ActorKind returns the "agent" or "human" prefix of an actor id, or "" if malformed.
func ActorKind(id string) string {
	if !actorIDRe.MatchString(id) {
		return ""
	}
	if len(id) > 6 && id[:6] == "agent:" {
		return "agent"
	}
	if len(id) > 6 && id[:6] == "human:" {
		return "human"
	}
	return ""
}

// ActorLocalID returns the part of an actor id after the "agent:"/"human:" prefix.
func ActorLocalID(id string) string {
	if len(id) > 6 && (id[:6] == "agent:" || id[:6] == "human:") {
		return id[6:]
	}
	return ""
}

// RegisterActor ensures the actor id exists in actors.json, creating it lazily.
// name is optional; if non-empty and the actor exists, the name is updated.
// Returns the (possibly updated) Actor.
func (s *Store) RegisterActor(id, name string) (Actor, error) {
	if err := ValidateActorID(id); err != nil {
		return Actor{}, err
	}
	var af actorsFile
	if err := ReadJSON(s.actorsPath(), &af); err != nil {
		return Actor{}, err
	}
	for i, a := range af.Actors {
		if a.ID == id {
			if name != "" && a.Name != name {
				af.Actors[i].Name = name
				if err := WriteJSON(s.actorsPath(), af); err != nil {
					return Actor{}, err
				}
			}
			return af.Actors[i], nil
		}
	}
	a := Actor{
		ID:        id,
		Kind:      ActorKind(id),
		Name:      name,
		FirstSeen: Now(),
	}
	af.Actors = append(af.Actors, a)
	// Keep actors sorted by id for deterministic output.
	sort.SliceStable(af.Actors, func(i, j int) bool { return af.Actors[i].ID < af.Actors[j].ID })
	if err := WriteJSON(s.actorsPath(), af); err != nil {
		return Actor{}, err
	}
	return a, nil
}

// ListActors returns all registered actors sorted by id.
func (s *Store) ListActors() ([]Actor, error) {
	var af actorsFile
	if err := ReadJSON(s.actorsPath(), &af); err != nil {
		return nil, err
	}
	sort.SliceStable(af.Actors, func(i, j int) bool { return af.Actors[i].ID < af.Actors[j].ID })
	return af.Actors, nil
}

// GetActor returns the actor with id, or ErrNotFound.
func (s *Store) GetActor(id string) (Actor, error) {
	actors, err := s.ListActors()
	if err != nil {
		return Actor{}, err
	}
	for _, a := range actors {
		if a.ID == id {
			return a, nil
		}
	}
	return Actor{}, fmt.Errorf("%w: actor %q", ErrNotFound, id)
}
