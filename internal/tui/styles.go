package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
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
	return truncate.String(s, uint(w))
}

func fitLineFrom(s string, start, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	pos := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if pos+rw <= start {
			pos += rw
			continue
		}
		if used+rw > width {
			break
		}
		out.WriteRune(r)
		used += rw
		pos += rw
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

// centerBlockBoth centers `s` both horizontally and vertically within a box of
// width w and height h (in lines). Used for empty-state copy that should sit
// in the middle of the dashboard content area.
func centerBlockBoth(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	maxL := 0
	for _, l := range lines {
		if lw := lipgloss.Width(l); lw > maxL {
			maxL = lw
		}
	}
	leftPad := 0
	if maxL < w {
		leftPad = (w - maxL) / 2
	}
	topPad := 0
	if n := len(lines); n < h {
		topPad = (h - n) / 2
	}
	out := make([]string, 0, topPad+len(lines))
	blank := spaces(w)
	for i := 0; i < topPad; i++ {
		out = append(out, blank)
	}
	left := spaces(leftPad)
	for _, l := range lines {
		out = append(out, left+l)
	}
	return strings.Join(out, "\n")
}

// centerLinesBoth top-pads pre-rendered lines to sit in the middle of an
// h-line box, while keeping the text left-aligned inside the pane. Returns the
// block without final height padding; callers pad to their content height so
// they can account for any header lines they wrote first.
func centerLinesBoth(lines []string, w, h int) string {
	if w < 1 {
		w = 1
	}
	topPad := 0
	if n := len(lines); n < h {
		topPad = (h - n) / 2
	}
	out := make([]string, 0, topPad+len(lines))
	blank := spaces(w)
	for i := 0; i < topPad; i++ {
		out = append(out, blank)
	}
	for _, l := range lines {
		out = append(out, fitLine(l, w))
	}
	return strings.Join(out, "\n")
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

func renderLabelChips(styles Styles, labels []string, width int) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, styles.LabelChip.Render(" "+label+" "))
	}
	line := strings.Join(parts, " ")
	if width > 0 && lipgloss.Width(line) > width {
		return strings.Join(labels, "   ")
	}
	return line
}

func dashboardContentWidth(width int) int {
	if width < 1 {
		return 1
	}
	contentW := width - 4
	if contentW < 1 {
		contentW = width
	}
	if contentW > 96 {
		contentW = 96
	}
	return contentW
}

func dashboardLeftPad(width int) int {
	return 0
}

func dashboardLine(width int, line string) string {
	contentW := dashboardContentWidth(width)
	prefix := spaces(dashboardLeftPad(width))
	if line == "" {
		return ""
	}
	return prefix + fitLine(line, contentW)
}

func dashboardBlock(width int, block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = dashboardLine(width, line)
	}
	return strings.Join(lines, "\n")
}

func sectionDivider(styles Styles, width int, title string) string {
	contentW := dashboardContentWidth(width)
	label := " " + title + " "
	labelW := lipgloss.Width(label)
	prefix := spaces(dashboardLeftPad(width))
	if labelW >= contentW {
		return prefix + styles.HeaderLabel.Render(fitLine(label, contentW))
	}
	fill := contentW - labelW
	left := fill / 2
	right := fill - left
	if left < 1 {
		left = 1
	}
	if right < 1 {
		right = 1
	}
	return prefix + styles.HeaderLabel.Render(repeat("─", left)+label+repeat("─", right))
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
