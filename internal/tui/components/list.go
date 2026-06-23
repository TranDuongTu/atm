package components

import (
	"fmt"
	"strings"
)

type ListItem struct {
	Key      string
	Title    string
	Subtitle string
	Detail   string
}

type List struct {
	Items    []ListItem
	Cursor   int
	Offset   int
	Height   int
	Width    int
	Filtered []ListItem
	Filter   string
}

func NewList(items []ListItem) *List {
	return &List{Items: items, Filtered: items, Height: 10, Width: 60}
}

func (l *List) SetSize(w, h int) {
	l.Width = w
	if h > 2 {
		l.Height = h - 2
	} else {
		l.Height = 1
	}
	l.clampCursor()
}

func (l *List) SetFilter(f string) {
	l.Filter = f
	if f == "" {
		l.Filtered = l.Items
		l.clampCursor()
		return
	}
	lower := strings.ToLower(f)
	var out []ListItem
	for _, it := range l.Items {
		if strings.Contains(strings.ToLower(it.Title), lower) ||
			strings.Contains(strings.ToLower(it.Key), lower) ||
			strings.Contains(strings.ToLower(it.Subtitle), lower) {
			out = append(out, it)
		}
	}
	l.Filtered = out
	if l.Cursor >= len(l.Filtered) {
		l.Cursor = max(0, len(l.Filtered)-1)
	}
}

func (l *List) MoveUp() {
	if l.Cursor > 0 {
		l.Cursor--
	}
	l.adjustOffset()
}

func (l *List) MoveDown() {
	if l.Cursor < len(l.Filtered)-1 {
		l.Cursor++
	}
	l.adjustOffset()
}

func (l *List) Top() {
	l.Cursor = 0
	l.Offset = 0
}

func (l *List) Bottom() {
	if len(l.Filtered) == 0 {
		l.Cursor = 0
		l.Offset = 0
		return
	}
	l.Cursor = len(l.Filtered) - 1
	l.adjustOffset()
}

func (l *List) Selected() (ListItem, bool) {
	if l.Cursor < 0 || l.Cursor >= len(l.Filtered) {
		return ListItem{}, false
	}
	return l.Filtered[l.Cursor], true
}

func (l *List) SetItems(items []ListItem) {
	l.Items = items
	l.SetFilter(l.Filter)
}

func (l *List) clampCursor() {
	if l.Cursor < 0 {
		l.Cursor = 0
	}
	if l.Cursor >= len(l.Filtered) {
		l.Cursor = max(0, len(l.Filtered)-1)
	}
	l.adjustOffset()
}

func (l *List) adjustOffset() {
	if l.Cursor < l.Offset {
		l.Offset = l.Cursor
	}
	if l.Cursor >= l.Offset+l.Height {
		l.Offset = l.Cursor - l.Height + 1
	}
	if l.Offset < 0 {
		l.Offset = 0
	}
}

func (l *List) Render() string {
	var b strings.Builder
	if len(l.Filtered) == 0 {
		b.WriteString("  (empty)\n")
		return b.String()
	}
	end := l.Offset + l.Height
	if end > len(l.Filtered) {
		end = len(l.Filtered)
	}
	for i := l.Offset; i < end; i++ {
		it := l.Filtered[i]
		cursor := "  "
		if i == l.Cursor {
			cursor = "> "
		}
		line := it.Key
		if it.Title != "" {
			if line != "" {
				line += "  "
			}
			line += it.Title
		}
		if it.Subtitle != "" {
			line += "  " + it.Subtitle
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, line))
	}
	if end < len(l.Filtered) {
		b.WriteString(fmt.Sprintf("  ... %d more\n", len(l.Filtered)-end))
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
