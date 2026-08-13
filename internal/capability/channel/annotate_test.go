package channel

import (
	"testing"

	"atm/internal/core"
)

func TestAnnotate(t *testing.T) {
	c := Cap{}
	if cell := c.Annotate(core.Task{ID: "t", Labels: []string{"ATM:status:open"}}); cell != nil {
		t.Fatalf("non-channel task: %v", cell)
	}
	payload, _ := core.EncodeChannelPayload(core.ChannelPayloadFrom(core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion}))
	cell := c.Annotate(core.Task{ID: "ATM-1", Labels: []string{"ATM:channel:notion"}, Meta: map[string]string{core.ChannelMetaKey: payload}})
	if cell == nil || cell.Text != "channel specs · notion" {
		t.Fatalf("cell: %+v", cell)
	}
	bad := c.Annotate(core.Task{ID: "ATM-2", Labels: []string{"ATM:channel:repo"}, Meta: map[string]string{core.ChannelMetaKey: "garbage"}})
	if bad == nil || bad.Tone != 2 { // capability.ToneAttention
		t.Fatalf("unreadable payload must degrade to an attention cell: %+v", bad)
	}
}
