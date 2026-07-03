package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("39")).
			Bold(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 1)

	// keyMenuStyle renders the contextual key hints in the bottom status bar.
	keyMenuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Bold(true)

	keyMenuDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// statusStyle renders the bottom status line text (STORE/SELECTED/actor).
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	statusLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).Bold(true)

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

	// rowCursorStyle highlights the cursor row with inverse video.
	rowCursorStyle = lipgloss.NewStyle().Reverse(true)

	// gutterSelectStyle renders the selection gutter marker.
	gutterSelectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

	// emptyStyle centers empty-state copy.
	emptyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// headerLabelStyle renders column header labels in the list views.
	headerLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

	// headerLineStyle renders the persistent Tasks-tab header line.
	headerLineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

	// groupHeaderStyle renders the grouped-view facet header (▾ LABEL (N)).
	groupHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

	// amberStyle renders warnings (retained_usage, remove confirm).
	amberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

	// toastStyle renders the transient toast message.
	toastStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("203")).
			Bold(true).
			Padding(0, 1)

	// helpTableStyle renders the parity table / keymap rows.
	helpTableStyle = lipgloss.NewStyle()

	// overlayBackdropStyle dims the underlying content when a form/overlay is open.
	overlayBackdropStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
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

// centerBlock right-pads and left-pads the (possibly multi-line) string so it
// is centered horizontally within width w.
func centerBlock(s string, w int) string {
	lines := strings.Split(s, "\n")
	maxL := 0
	for _, l := range lines {
		if lw := lipgloss.Width(l); lw > maxL {
			maxL = lw
		}
	}
	if maxL >= w {
		return s
	}
	pad := (w - maxL) / 2
	left := spaces(pad)
	for i, l := range lines {
		lines[i] = left + l
	}
	return strings.Join(lines, "\n")
}

// truncateRunes truncates s to at most w display columns, appending an
// ellipsis ("...") if it was shortened.
func truncateRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 3 {
		return fitLine(s, w)
	}
	return fitLine(s, w-3) + "..."
}

// repeat returns n copies of s, clamped to n>=0 (avoids the panic
// strings.Repeat panics on negative counts when the terminal width is 0).
func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// sepLine returns a horizontal separator line of length max(1, min(cap,
// width-sub)), clamped to >=1 so a 0-width terminal never panics.
func sepLine(rune_ string, cap, width, sub int) string {
	n := cap
	if w := width - sub; w < n {
		n = w
	}
	if n < 1 {
		n = 1
	}
	return repeat(rune_, n)
}

// relTime renders a human-readable relative timestamp from t to now.
func relTime(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}
