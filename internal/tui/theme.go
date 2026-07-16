package tui

import "github.com/charmbracelet/lipgloss"

type ThemeName string

const (
	themeGraphite ThemeName = "graphite"
	themeLight    ThemeName = "light"
	themeMono     ThemeName = "mono"
)

var themeOrder = []ThemeName{themeGraphite, themeLight, themeMono}

type Theme struct {
	Text       lipgloss.Color
	Muted      lipgloss.Color
	Subtle     lipgloss.Color
	Surface    lipgloss.Color
	Border     lipgloss.Color
	Accent     lipgloss.Color
	AccentText lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
}

type Styles struct {
	ActiveTab       lipgloss.Style
	InactiveTab     lipgloss.Style
	PaneActive      lipgloss.Style
	PaneInactive    lipgloss.Style
	KeyMenu         lipgloss.Style
	KeyMenuDim      lipgloss.Style
	Status          lipgloss.Style
	StatusLabel     lipgloss.Style
	StatusOK        lipgloss.Style
	Dialog          lipgloss.Style
	DialogTitle     lipgloss.Style
	DialogBody      lipgloss.Style
	FieldLabel      lipgloss.Style
	FieldValue      lipgloss.Style
	FieldHint       lipgloss.Style
	ButtonActive    lipgloss.Style
	ButtonInactive  lipgloss.Style
	RowCursor       lipgloss.Style
	GutterSelect    lipgloss.Style
	EmptyHead       lipgloss.Style
	EmptyText       lipgloss.Style
	EmptyKey        lipgloss.Style
	EmptyDim        lipgloss.Style
	HeaderLabel     lipgloss.Style
	HeaderLine      lipgloss.Style
	GroupHeader     lipgloss.Style
	NamespaceHeader lipgloss.Style
	LabelChip       lipgloss.Style
	PinPill         lipgloss.Style
	Muted           lipgloss.Style
	Body            lipgloss.Style
	Warning         lipgloss.Style
	Error           lipgloss.Style
	Success         lipgloss.Style
	Toast           lipgloss.Style
	OverlayBackdrop lipgloss.Style
	HelpTable       lipgloss.Style
}

func defaultThemeName() ThemeName { return themeGraphite }

func nextThemeName(current ThemeName) ThemeName {
	for i, name := range themeOrder {
		if current == name {
			return themeOrder[(i+1)%len(themeOrder)]
		}
	}
	return defaultThemeName()
}

func themeByName(name ThemeName) Theme {
	switch name {
	case themeGraphite:
		return Theme{Text: "252", Muted: "244", Subtle: "238", Surface: "235", Border: "242", Accent: "214", AccentText: "0", Warning: "214", Error: "203", Success: "113"}
	case themeLight:
		return Theme{Text: "235", Muted: "244", Subtle: "250", Surface: "255", Border: "245", Accent: "25", AccentText: "255", Warning: "130", Error: "160", Success: "28"}
	case themeMono:
		return Theme{Text: "255", Muted: "250", Subtle: "240", Surface: "0", Border: "255", Accent: "255", AccentText: "0", Warning: "255", Error: "255", Success: "255"}
	default:
		return themeByName(defaultThemeName())
	}
}

func buildStyles(themeName ThemeName) Styles {
	t := themeByName(themeName)
	s := Styles{
		ActiveTab:       lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Accent).Bold(true).Padding(0, 1),
		InactiveTab:     lipgloss.NewStyle().Foreground(t.Muted).Padding(0, 1),
		PaneActive:      lipgloss.NewStyle().Foreground(t.Text).BorderForeground(t.Accent),
		PaneInactive:    lipgloss.NewStyle().Foreground(t.Muted).BorderForeground(t.Border),
		KeyMenu:         lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		KeyMenuDim:      lipgloss.NewStyle().Foreground(t.Muted),
		Status:          lipgloss.NewStyle().Foreground(t.Muted),
		StatusLabel:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		StatusOK:        lipgloss.NewStyle().Foreground(t.Success).Bold(true),
		Dialog:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).Padding(0, 1),
		DialogTitle:     lipgloss.NewStyle().Foreground(t.Text).Bold(true).Padding(0, 1),
		DialogBody:      lipgloss.NewStyle().Foreground(t.Text),
		FieldLabel:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		FieldValue:      lipgloss.NewStyle().Foreground(t.Text),
		FieldHint:       lipgloss.NewStyle().Foreground(t.Muted).Italic(true),
		ButtonActive:    lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Accent).Bold(true).Padding(0, 2),
		ButtonInactive:  lipgloss.NewStyle().Foreground(t.Muted).Padding(0, 2),
		RowCursor:       lipgloss.NewStyle().Reverse(true),
		GutterSelect:    lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		EmptyHead:       lipgloss.NewStyle().Foreground(t.Text).Bold(true),
		EmptyText:       lipgloss.NewStyle().Foreground(t.Text),
		EmptyKey:        lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		EmptyDim:        lipgloss.NewStyle().Foreground(t.Muted),
		HeaderLabel:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		HeaderLine:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		GroupHeader:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		NamespaceHeader: lipgloss.NewStyle().Foreground(t.Text).Bold(true),
		LabelChip:       lipgloss.NewStyle().Foreground(t.Text).Background(t.Subtle).Padding(0, 1),
		PinPill:         lipgloss.NewStyle().Foreground(t.Text).Border(lipgloss.RoundedBorder(), false, true).BorderForeground(t.Border),
		Muted:           lipgloss.NewStyle().Foreground(t.Muted),
		Body:            lipgloss.NewStyle().Foreground(t.Text),
		Warning:         lipgloss.NewStyle().Foreground(t.Warning).Bold(true),
		Error:           lipgloss.NewStyle().Foreground(t.Error).Bold(true),
		Success:         lipgloss.NewStyle().Foreground(t.Success).Bold(true),
		Toast:           lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Error).Bold(true).Padding(0, 1),
		OverlayBackdrop: lipgloss.NewStyle().Foreground(t.Subtle),
		HelpTable:       lipgloss.NewStyle().Foreground(t.Text),
	}
	if themeName == themeMono {
		s.ActiveTab = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
		s.PaneActive = lipgloss.NewStyle().Bold(true).BorderForeground(t.Accent)
		s.PaneInactive = lipgloss.NewStyle().BorderForeground(t.Border)
		s.HeaderLabel = lipgloss.NewStyle().Bold(true).Underline(true)
		s.GroupHeader = lipgloss.NewStyle().Bold(true)
		s.LabelChip = lipgloss.NewStyle().Reverse(true).Padding(0, 1)
		s.StatusOK = lipgloss.NewStyle().Bold(true)
	}
	return s
}
