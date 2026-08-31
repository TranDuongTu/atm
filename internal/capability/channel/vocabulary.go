package channel

import "atm/internal/core"

const CapabilityName = "channel"

// BoardChannels is the one board this capability owns: every channel record.
func BoardChannels(code string) string { return code + ":channels" }

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":channel:*", Description: "channel records: how personas communicate with each other and humans. Each member task is one channel — its title is the handle, its description the purpose, and its payload the type-shaped address. Managed by `atm channel`; do not hand-edit the payload."},
		{Name: core.ChannelLabel(code, core.ChannelTypeRepo), Description: "a git repository channel: the address is the remote URL; this machine's clone path lives in local wiring (config, not synced)"},
		{Name: core.ChannelLabel(code, core.ChannelTypeNotion), Description: "a Notion channel: the address names the workspace and database/page; agents reach it through their own MCP server, never through ATM, and ATM stores no credentials"},
		{Name: core.ChannelLabel(code, core.ChannelTypeSlack), Description: "a Slack channel: the address names the workspace slug and the channel id; agents reach it through their own MCP server, never through ATM, and ATM stores no credentials"},
		{Name: BoardChannels(code), Description: "every channel record in the project. Browse channels with `atm channel list` or the TUI channels overlay; this board exists so queries and other boards can see them.", Expr: "channel:*"},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// EnsureVocabulary seeds the labels and board in one LabelSeedBatch
// transaction and returns the board labels it owns.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	vocab := vocabulary(code)
	if err := s.LabelSeedBatch(vocab, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range vocab {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
