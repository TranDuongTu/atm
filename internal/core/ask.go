package core

// AskTurn is one completed exchange in an `atm ask --session` conversation:
// the question asked and the answer given, replayed to the model as history on
// the next turn.
//
// It lives in core, not in internal/store, because internal/cli may not import
// internal/store (tests/arch's TestCLIDoesNotImportStore) and ReadAskTurns
// returns it. AppendInquiry avoids the problem by taking primitives — a writer
// can, a reader cannot.
type AskTurn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	At       string `json:"at"`
}
