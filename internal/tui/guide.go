package tui

func d_app_setDashProject(app *Model, code string) {
	app.dash.refresh()
	app.tab = tabDashboard
	app.showToast("dashboard scoped to " + code)
}
