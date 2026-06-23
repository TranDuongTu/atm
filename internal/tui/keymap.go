package tui

type keymap struct {
	switchTab1    string
	switchTab2    string
	switchTab3    string
	switchTab4    string
	switchTab5    string
	tabNext       string
	tabPrev       string
	refresh       string
	quit          string
	help          string
	palette       string
	filter        string
	up            string
	down          string
	top           string
	bottom        string
	enter         string
	add           string
	edit          string
	remove        string
	toggle        string
	escape        string
	dashboardA    string
	dashboardR    string
	dashboardBigR string
	dashboardE    string
	dashboardP    string
	projectN      string
	projectT      string
	projectBigL   string
	projectL      string
	projectBigR   string
	projectR      string
	guideBigS     string
	guideS        string
	guideBigX     string
	guideBigM     string
	guideG        string
	guideM        string
	guideD        string
	guideBigF     string
	guideBigD     string
	taskN         string
	taskC         string
	taskU         string
	taskS         string
	taskE         string
	taskB         string
	taskBigL      string
	taskT         string
	taskO         string
	taskBigO      string
	taskD         string
	taskV         string
}

func defaultKeymap() keymap {
	return keymap{
		switchTab1:    "1",
		switchTab2:    "2",
		switchTab3:    "3",
		switchTab4:    "4",
		switchTab5:    "5",
		tabNext:       "tab",
		tabPrev:       "shift+tab",
		refresh:       "r",
		quit:          "q",
		help:          "?",
		palette:       ":",
		filter:        "/",
		up:            "up",
		down:          "down",
		top:           "g",
		bottom:        "G",
		enter:         "enter",
		add:           "a",
		edit:          "e",
		remove:        "x",
		toggle:        " ",
		escape:        "esc",
		dashboardA:    "a",
		dashboardR:    "r",
		dashboardBigR: "R",
		dashboardE:    "E",
		dashboardP:    "P",
		projectN:      "N",
		projectT:      "T",
		projectBigL:   "L",
		projectL:      "l",
		projectBigR:   "R",
		projectR:      "r",
		guideBigS:     "S",
		guideS:        "s",
		guideBigX:     "X",
		guideBigM:     "M",
		guideG:        "g",
		guideM:        "m",
		guideD:        "d",
		guideBigF:     "F",
		guideBigD:     "D",
		taskN:         "n",
		taskC:         "c",
		taskU:         "u",
		taskS:         "s",
		taskE:         "e",
		taskB:         "b",
		taskBigL:      "L",
		taskT:         "t",
		taskO:         "o",
		taskBigO:      "O",
		taskD:         "d",
		taskV:         "v",
	}
}

func keyMatch(k keymap, msgKey string, field string) bool {
	return msgKey == field
}
