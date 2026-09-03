package profile

import (
	"strings"
	"testing"
	"testing/fstest"
)

// goodFiles is a minimal but complete profile in the documented layout: one
// manifest, two personas, two checklists (one project-target, one
// task-target), one channel. Tests mutate a copy of it to build each
// rejection case, so every failure names exactly one broken rule.
func goodFiles() fstest.MapFS {
	return fstest.MapFS{
		"manifest.yaml": &fstest.MapFile{Data: []byte(`name: scrumban
version: 1.0.0
format: 1
description: >
  ATM's standard operating model: weekly planning and
  design-gated incremental implementation.
authors: [ATM]
requires_capabilities: [scrum, channel]
`)},
		"personas/manager.md": &fstest.MapFile{Data: []byte(`---
name: manager
description: Runs the flow.
---
# Persona: manager

You are the manager of <CODE>.

## Principles

1. **Multiply, don't produce.**
`)},
		"personas/developer.md": &fstest.MapFile{Data: []byte(`---
name: developer
description: Implements features.
---
# Persona: developer

You build things.
`)},
		"checklists/planning.md": &fstest.MapFile{Data: []byte(`---
name: planning
purpose: the weekly planning pass for <CODE>
suits: [manager]
requires_capabilities: [scrum, channel]
requires_channels: [planning]
target: project
mode: eager
---
1. Orient before deciding anything.
   1. Scan #planning over the last week.
   2. Scan #standup since the previous plan.
2. Sweep each enabled flow capability.
`)},
		"checklists/scrum-coding.md": &fstest.MapFile{Data: []byte(`---
name: scrum-coding
purpose: implement one increment
suits: [developer]
requires_capabilities: [scrum]
target: task
targets: "(<CODE>:scrum:task OR <CODE>:scrum:bug) AND <CODE>:scrum-stage:implementable"
mode: interactive
---
1. Gate.
2. Implement.
`)},
		"channels/planning.md": &fstest.MapFile{Data: []byte(`---
name: planning
role_hint: home
---
The weekly plan and its discussion for <CODE>.
`)},
	}
}

// withFile returns the good profile with one file replaced or added.
func withFile(name, body string) fstest.MapFS {
	fsys := goodFiles()
	fsys[name] = &fstest.MapFile{Data: []byte(body)}
	return fsys
}

// withoutFile returns the good profile with one file removed.
func withoutFile(name string) fstest.MapFS {
	fsys := goodFiles()
	delete(fsys, name)
	return fsys
}

// wantLoadErr asserts Load rejects fsys with a message mentioning want.
func wantLoadErr(t *testing.T, fsys fstest.MapFS, want string) {
	t.Helper()
	_, err := Load(fsys)
	if err == nil {
		t.Fatalf("Load succeeded, want error mentioning %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Load error = %v, want it to mention %q", err, want)
	}
}
