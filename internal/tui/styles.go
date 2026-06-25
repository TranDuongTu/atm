package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	topStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39"))

	contentStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("245"))

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("245"))

	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39"))

	bottomStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39"))

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("39")).
			Bold(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 2)

	locationStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Italic(true)

	// keyMenuStyle renders the contextual key hints in the bottom status bar.
	keyMenuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Bold(true)

	keyMenuDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// dialogStyle renders the create/edit dialog as a bordered overlay with
	// no opaque background so the underlying content shows through.
	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1)

	dialogTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Padding(0, 1)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	fieldValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	fieldHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).Italic(true)

	buttonActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("39")).
				Bold(true).
				Padding(0, 2)

	buttonInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 2)
)

// box renders `inner` inside a bordered block at least `w` columns wide (so
// short content is padded to span the terminal, while long content overflows
// the border naturally instead of being wrapped/truncated). Each line of
// `inner` is right-padded to the inner width when shorter; longer lines are
// left untouched so substring searches on header content stay intact.
func box(style lipgloss.Style, w int, inner string) string {
	innerW := w - 2 // border left + right
	if innerW < 1 {
		innerW = 1
	}
	return style.Render(padBlock(inner, innerW))
}

func titledBox(style lipgloss.Style, w int, title, inner string) string {
	innerW := w - 2 // border left + right
	if innerW < 1 {
		innerW = 1
	}
	rendered := style.Render(padBlock(inner, innerW))
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}
	runes := []rune(lines[0])
	label := []rune(" " + title + " ")
	if len(runes) > len(label)+2 {
		for i, r := range label {
			runes[i+1] = r
		}
		lines[0] = string(runes)
	}
	return strings.Join(lines, "\n")
}

func padBlock(s string, w int) string {
	out := []byte{}
	lineStart := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[lineStart:i]
			if lw := lipgloss.Width(line); lw < w {
				line = line + spaces(w-lw)
			}
			out = append(out, line...)
			if i < len(s) {
				out = append(out, '\n')
			}
			lineStart = i + 1
		}
	}
	return string(out)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
