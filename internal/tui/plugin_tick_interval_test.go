package tui

import (
	"testing"
	"time"
)

// The plugin tick's only job with no overlay open is draining im.msgCh so
// dock state stays roughly current — but each tick forces a full View
// render, so a background watcher at the overlay cadence burned ~8 renders/s
// forever (ATM-4c476c). The interval must be fast only while the overlay is
// actually showing the live log.
func TestPluginTickIntervalByOverlayState(t *testing.T) {
	m := newIndexerTestModel(t)

	m.pluginOverlay = 0 // overlay open: live log tailing wants a snappy tick
	if d := pluginTickInterval(m); d != 120*time.Millisecond {
		t.Fatalf("overlay open: interval %v, want 120ms", d)
	}

	m.pluginOverlay = -1 // background watcher only: relax
	if d := pluginTickInterval(m); d < 500*time.Millisecond {
		t.Fatalf("no overlay: interval %v, want >= 500ms", d)
	}
}
