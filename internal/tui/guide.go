package tui

func d_app_setDashProject(app *Model, code string) {
	app.projectScope = code
	app.dash.refresh()
	app.focused = paneSummary
	app.showToast("summary scoped to " + code)
}
