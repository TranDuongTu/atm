package answer

// Turn is one completed exchange, replayed to the model as history.
type Turn struct {
	Question string
	Answer   string
}

// Query is one ask: the new question plus the conversation it continues.
type Query struct {
	Question string
	History  []Turn
}
