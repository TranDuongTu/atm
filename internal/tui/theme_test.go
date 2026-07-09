package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestStatusOKStylePresentInAllThemes verifies the StatusOK style exists on
// every theme and carries the intended color semantics: a real foreground
// color (green-leaning, from Theme.Success) for graphite and light, and a
// bold-only override (NoColor) for the no-color mono theme.
func TestStatusOKStylePresentInAllThemes(t *testing.T) {
	for _, name := range []ThemeName{themeGraphite, themeLight, themeMono} {
		s := buildStyles(name)
		fg := s.StatusOK.GetForeground()
		switch name {
		case themeGraphite, themeLight:
			if _, ok := fg.(lipgloss.NoColor); ok {
				t.Errorf("theme %s: StatusOK foreground is NoColor, expected a real color", name)
			}
		case themeMono:
			if _, ok := fg.(lipgloss.NoColor); !ok {
				t.Errorf("theme %s: StatusOK foreground should be NoColor (bold-only), got %T (%v)", name, fg, fg)
			}
		}
	}
}
