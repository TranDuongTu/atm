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
			Foreground(lipgloss.Color("245"))

	activePaneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

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
	return titledBoxHeight(style, w, title, inner, 0)
}

func titledBoxHeight(style lipgloss.Style, w int, title, inner string, height int) string {
	innerW := w - 2 // border left + right
	if innerW < 1 {
		innerW = 1
	}
	if height > 0 && height < 3 {
		height = 3
	}
	bodyLines := strings.Split(strings.TrimRight(inner, "\n"), "\n")
	if len(bodyLines) == 1 && bodyLines[0] == "" {
		bodyLines = []string{""}
	}
	innerH := len(bodyLines)
	if height > 0 {
		innerH = height - 2
	}
	if innerH < 1 {
		innerH = 1
	}
	if len(bodyLines) > innerH {
		bodyLines = bodyLines[:innerH]
	}
	for len(bodyLines) < innerH {
		bodyLines = append(bodyLines, "")
	}

	label := " " + title + " "
	topFill := innerW
	if lw := lipgloss.Width(label); lw < topFill {
		label += strings.Repeat("─", topFill-lw)
	} else {
		label = fitLine(label, topFill)
	}
	top := style.Render("╭" + label + "╮")
	bottom := style.Render("╰" + strings.Repeat("─", innerW) + "╯")
	lines := []string{top}
	for _, line := range bodyLines {
		fit := fitLine(line, innerW)
		if lw := lipgloss.Width(fit); lw < innerW {
			fit += spaces(innerW - lw)
		}
		lines = append(lines, style.Render("│")+fit+style.Render("│"))
	}
	lines = append(lines, bottom)
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

func fitLine(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	var out strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w {
			break
		}
		out.WriteRune(r)
		used += rw
	}
	return out.String()
}
