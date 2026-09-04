package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	mdstyles "github.com/charmbracelet/glamour/styles"
)

// markdownRenderer is the one method the TUI needs from glamour, named as an
// interface so the degrade path has something to fail through in tests.
type markdownRenderer interface {
	Render(string) (string, error)
}

// newMarkdownRenderer builds the renderer drill-ins read through. It is a
// var so a test can watch what happens when there is no renderer to be had.
var newMarkdownRenderer = func(width int) (markdownRenderer, error) {
	// The dark style with its document background and margin removed: the
	// modal already owns its background and its indent, and glamour painting
	// its own over the top is what makes an embedded block look pasted in.
	style := mdstyles.DarkStyleConfig
	style.Document.BackgroundColor = nil
	style.Document.Margin = uintPtr(0)
	return glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
}

func uintPtr(v uint) *uint { return &v }

// renderMarkdown renders src for a pane width columns wide, as lines ready
// to be windowed by the scroll offset.
//
// Every failure degrades to the raw source rather than to a blank pane: a
// comment body is the reader's data, and showing it unstyled beats showing
// nothing because a renderer could not be built.
func renderMarkdown(src string, width int) []string {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	if width < 20 {
		width = 20
	}
	r, err := newMarkdownRenderer(width)
	if err != nil {
		return strings.Split(src, "\n")
	}
	out, err := r.Render(src)
	if err != nil {
		return strings.Split(src, "\n")
	}
	// glamour pads the block with blank lines top and bottom; the page owns
	// its own spacing, so they are trimmed rather than rendered twice.
	return strings.Split(strings.Trim(out, "\n"), "\n")
}
