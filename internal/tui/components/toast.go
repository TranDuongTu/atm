package components

import "time"

type Toast struct {
	Message string
	At      time.Time
	Visible bool
}

func NewToast() *Toast { return &Toast{} }

func (t *Toast) Show(msg string) {
	t.Message = msg
	t.At = time.Now()
	t.Visible = true
}

func (t *Toast) Hide() { t.Visible = false }

func (t *Toast) Render() string {
	if !t.Visible || t.Message == "" {
		return ""
	}
	return "[ " + t.Message + " ]"
}
