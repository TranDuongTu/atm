// internal/store/channel.go
package store

import (
	"fmt"
	"os"
	"path/filepath"

	"atm/internal/core"
)

// channelTypeValid reports whether typ is a recognized channel type.
func channelTypeValid(typ string) bool {
	for _, t := range core.ChannelTypes {
		if t == typ {
			return true
		}
	}
	return false
}

// ChannelRecords lists the project's tier-1 channel records, decoding every
// task in the channel:* namespace. Tasks with unreadable payloads are skipped
// here — list degrades rather than failing whole; the capability's Annotate
// cell and a by-name lookup of that record surface the breakage.
func (s *Store) ChannelRecords(code string) ([]core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "channel:*"})
	if err != nil {
		return nil, err
	}
	var out []core.ChannelRecord
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil || rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

// channelByName resolves a handle to its record; core.ErrNotFound when absent,
// and a decode error ONLY when the named channel's own payload is unreadable.
// A corrupt record must not poison lookups of its neighbours: the handle a
// caller asked for either resolves or is absent, no matter what else is in
// the namespace. When the payload is unreadable the handle is unknowable, so
// the task's TITLE is the fallback identity — that is how `atm channel` can
// still name and remove the broken record.
func (s *Store) channelByName(code, name string) (*core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "channel:*"})
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil {
			if t.Title == name {
				return nil, err // the one they asked for is the broken one
			}
			continue
		}
		if rec != nil && rec.Name == name {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// CreateChannel authors the tier-1 record: a task titled by the handle,
// labelled channel:<type>, description = purpose, payload = name/type/address.
// The handle must be unique across the project's channels (all types).
func (s *Store) CreateChannel(code string, rec core.ChannelRecord, actor string) (*core.Task, error) {
	if rec.Name == "" || !channelTypeValid(rec.Type) {
		return nil, fmt.Errorf("%w: channel needs a name and a type in %v", core.ErrUsage, core.ChannelTypes)
	}
	existing, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Name == rec.Name {
			return nil, fmt.Errorf("%w: channel %q already exists (task %s)", core.ErrUsage, rec.Name, e.TaskID)
		}
	}
	t, err := s.CreateTask(code, rec.Name, rec.Purpose, []string{core.ChannelLabel(code, rec.Type)}, actor)
	if err != nil {
		return nil, err
	}
	payload, err := core.EncodeChannelPayload(core.ChannelPayloadFrom(rec))
	if err != nil {
		return nil, err
	}
	if err := s.SetTaskCapabilityMeta(t.ID, core.ChannelMetaKey, payload, actor); err != nil {
		return nil, err
	}
	return s.GetTask(t.ID)
}

// EditChannel updates purpose and/or address. Address writes decode the
// existing payload and mutate only the address key, so unknown fields from
// newer binaries survive (degrade-never-reject applied to ourselves). An
// address with every field empty CLEARS the key rather than writing a null.
// There is no rename: the handle is the channel's identity, referenced by
// tier-2 wiring keys and by agents that resolved it once — re-authoring a
// channel under a new handle is `remove` + `add`.
func (s *Store) EditChannel(code, name string, purpose *string, addr *core.ChannelAddress, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if purpose != nil {
		if err := s.SetDescription(rec.TaskID, *purpose, actor); err != nil {
			return err
		}
	}
	if addr != nil {
		t, err := s.GetTask(rec.TaskID)
		if err != nil {
			return err
		}
		m, err := core.DecodeChannelPayload(t.Meta[core.ChannelMetaKey])
		if err != nil {
			return err
		}
		next := *rec
		next.Address = *addr
		if a, ok := core.ChannelPayloadFrom(next)["address"]; ok {
			m["address"] = a
		} else {
			delete(m, "address")
		}
		m["name"], m["type"] = rec.Name, rec.Type
		enc, err := core.EncodeChannelPayload(m)
		if err != nil {
			return err
		}
		if err := s.SetTaskCapabilityMeta(rec.TaskID, core.ChannelMetaKey, enc, actor); err != nil {
			return err
		}
	}
	return nil
}

// RemoveChannel removes the ledger record (task tombstone) and drops this
// machine's wiring entry for the handle, if any.
func (s *Store) RemoveChannel(code, name, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if err := s.RemoveTask(rec.TaskID, actor); err != nil {
		return err
	}
	return s.dropChannelWiring(code, name, actor)
}

// SetChannelWiring records how THIS machine reaches the channel — tier 2:
// config, not substrate, no event, no secrets. Merge semantics: a non-empty
// path or mcpServer overwrites that field, an empty one keeps the existing
// value, and stamps always survive. The channel must exist in the ledger.
func (s *Store) SetChannelWiring(code, name, path, mcpServer, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.channelByName(code, name); err != nil {
		return err
	}
	abs := ""
	if path != "" {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve channel path: %w", err)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return fmt.Errorf("%w: channel path does not exist or is not a directory: %s", core.ErrUsage, abs)
		}
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		if abs != "" {
			w.Path = abs
		}
		if mcpServer != "" {
			w.MCPServer = mcpServer
		}
		merged.Channels[name] = w
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// AddChannelStamp appends a verification stamp (actor + timestamp + note) to
// the channel's wiring: "someone actually touched this channel and vouches".
func (s *Store) AddChannelStamp(code, name, note, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.channelByName(code, name); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		w.Stamps = append(w.Stamps, core.VerificationStamp{At: core.RFC3339UTC(core.Now()), By: actor, Note: note})
		merged.Channels[name] = w
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// lockedProjectConfig is the read half of every config read-modify-write in
// this file: current config or a fresh zero value. Call under WithLock only.
func (s *Store) lockedProjectConfig(code string) (*ProjectConfig, error) {
	existing, err := s.GetProjectConfig(code)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return &ProjectConfig{}, nil
}

// dropChannelWiring removes the handle's wiring entry, silently succeeding
// when there is none (RemoveChannel's local cleanup).
func (s *Store) dropChannelWiring(code, name, actor string) error {
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if _, ok := merged.Channels[name]; !ok {
			return nil
		}
		delete(merged.Channels, name)
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}
