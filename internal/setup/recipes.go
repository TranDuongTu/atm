package setup

// Recipe is everything an MCP adapter needs to add a server via the
// harness's own `mcp add` verb. ATM never writes a harness config file
// itself — Recipe only carries the argv-building inputs; the harness owns
// storage and any credential prompt.
type Recipe struct {
	Server    string
	Transport string
	URL       string
	NeedsAuth bool
}
