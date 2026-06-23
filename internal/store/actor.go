package store

import (
	"fmt"
	"sort"
	"time"
)

type Actor struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
}

type actorsFile struct {
	Actors []Actor `json:"actors"`
}

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

func ActorLocalID(id string) string {
	if len(id) > 6 && (id[:6] == "agent:" || id[:6] == "human:") {
		return id[6:]
	}
	return ""
}

func (s *Store) Register(id, name string) error {
	if err := ValidateActorID(id); err != nil {
		return err
	}
	var af actorsFile
	if err := ReadJSON(s.actorsPath(), &af); err != nil {
		return err
	}
	for i, a := range af.Actors {
		if a.ID == id {
			if name != "" && a.Name != name {
				af.Actors[i].Name = name
				return WriteJSON(s.actorsPath(), af)
			}
			return nil
		}
	}
	a := Actor{
		ID:        id,
		Kind:      ActorKind(id),
		Name:      name,
		FirstSeen: Now(),
	}
	af.Actors = append(af.Actors, a)
	sort.SliceStable(af.Actors, func(i, j int) bool { return af.Actors[i].ID < af.Actors[j].ID })
	return WriteJSON(s.actorsPath(), af)
}

func (s *Store) List() []Actor {
	var af actorsFile
	if err := ReadJSON(s.actorsPath(), &af); err != nil {
		return nil
	}
	sort.SliceStable(af.Actors, func(i, j int) bool { return af.Actors[i].ID < af.Actors[j].ID })
	return af.Actors
}

func (s *Store) Get(id string) (Actor, error) {
	for _, a := range s.List() {
		if a.ID == id {
			return a, nil
		}
	}
	return Actor{}, fmt.Errorf("%w: actor %q", ErrNotFound, id)
}

func (s *Store) RegisterActor(id, name string) (Actor, error) {
	if err := s.Register(id, name); err != nil {
		return Actor{}, err
	}
	return s.Get(id)
}

func (s *Store) ListActors() ([]Actor, error) { return s.List(), nil }

func (s *Store) GetActor(id string) (Actor, error) { return s.Get(id) }
