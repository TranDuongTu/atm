package store

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/seed"
)

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}

// LabelAdd is the explicit "force upsert" path for a label: it always
// appends a label.upserted event to the project's log, then write-throughs
// the cache row. If `description` is empty, the existing description on the
// live row (if any) is preserved; a non-empty description overwrites it.
// Contrast with LabelSeed, which is a no-op when the label already exists.
func (s *Store) LabelAdd(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	code := labelProject(name)
	return s.WithLock(code, func() error {
		l := Label{Name: name, Description: description}
		if description == "" {
			if existing, ok, err := cacheGetLabel(db, name); err != nil {
				return err
			} else if ok {
				l.Description = existing.Description
			}
		}
		now := Now()
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(l),
		})
		if err != nil {
			return err
		}
		l.LogSeq = entry.Seq
		return cacheUpsertLabel(db, l)
	})
}

// LabelSeed upserts a label but only sets the description when the label is
// newly created. Existing labels keep their descriptions.
func (s *Store) LabelSeed(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	code := labelProject(name)
	return s.WithLock(code, func() error {
		if _, ok, err := cacheGetLabel(db, name); err != nil {
			return err
		} else if ok {
			return nil
		}
		now := Now()
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(Label{Name: name, Description: description}),
		})
		if err != nil {
			return err
		}
		return cacheUpsertLabel(db, Label{Name: name, Description: description, LogSeq: entry.Seq})
	})
}

// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent.
func (s *Store) SeedLabels(code, actor string) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		if err := s.LabelSeed(full, l.Description, actor); err != nil {
			return err
		}
	}
	return nil
}

// seedLabelsLocked appends label.upserted events for each default label not
// already live, write-throughing each cache row. Caller MUST hold the
// project lock. Called by CreateProject from inside its own WithLock.
func (s *Store) seedLabelsLocked(code, actor string, at time.Time) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      at,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: full},
			Payload: mustMarshal(Label{Name: full, Description: l.Description}),
		})
		if err != nil {
			return err
		}
		if err := cacheUpsertLabel(db, Label{Name: full, Description: l.Description, LogSeq: entry.Seq}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LabelRemove(name, actor string) (*LabelRemoveResult, error) {
	if err := ValidateLabelName(name); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var result *LabelRemoveResult
	code := labelProject(name)
	err = s.WithLock(code, func() error {
		l, ok, err := cacheGetLabel(db, name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: label %q", ErrNotFound, name)
		}
		now := Now()
		_, err = s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionLabelRemoved,
			Subject: Subject{Kind: "label", Name: name},
			Payload: mustMarshal(l),
		})
		if err != nil {
			return err
		}
		if err := cacheDeleteLabel(db, name); err != nil {
			return err
		}
		count, err := cacheCountTasksWithLabelGlobally(db, name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	return result, err
}

func (s *Store) LabelList(project, namespace string) []Label {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	out, err := cacheListLabels(db, project, namespace)
	if err != nil {
		return nil
	}
	return out
}

func (s *Store) LabelShow(name string) (Label, error) {
	db, err := s.cacheDB()
	if err != nil {
		return Label{}, err
	}
	l, ok, err := cacheGetLabel(db, name)
	if err != nil {
		return Label{}, err
	}
	if !ok {
		return Label{}, fmt.Errorf("%w: label %q", ErrNotFound, name)
	}
	return l, nil
}

func (s *Store) Namespaces(code string) []string {
	db, err := s.cacheDB()
	if err != nil {
		return nil
	}
	ns, err := cacheNamespaces(db, code)
	if err != nil {
		return nil
	}
	return ns
}

func (s *Store) labelProjectExists(name string) error {
	code := labelProject(name)
	if _, err := s.GetProject(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", ErrUsage, code, name)
	}
	return nil
}

// labelProjectExistsLocked is identical to labelProjectExists except that it
// calls getProjectLocked instead of GetProject. It exists ONLY for callers
// that already hold the label's project lock (i.e. are running inside their
// own s.WithLock(code, ...) closure) — calling labelProjectExists in that
// situation would re-enter the (non-reentrant) mutex via GetProject and
// deadlock. Used by CreateTask, which validates supplied labels from inside
// its own WithLock closure.
func (s *Store) labelProjectExistsLocked(name string) error {
	code := labelProject(name)
	if _, err := s.getProjectLocked(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", ErrUsage, code, name)
	}
	return nil
}

func labelProject(name string) string {
	return strings.SplitN(name, ":", 2)[0]
}

// LabelUsage counts entities in the given project carrying the label —
// tasks plus comments. Exported for the TUI's Labels pane "(N entities)"
// suffix and the CLI's retained_usage report. A label like
// <CODE>:comment:open-question can have zero tasks but many comments, so
// counting only tasks understated real adoption. Backed by two indexed
// COUNT queries — see docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md
// and ATM-0027-c0003.
func (s *Store) LabelUsage(projectCode, label string) (int, error) {
	db, err := s.cacheDB()
	if err != nil {
		return 0, err
	}
	return cacheLabelUsage(db, projectCode, label)
}
