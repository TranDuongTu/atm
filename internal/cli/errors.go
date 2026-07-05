package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"atm/internal/store"
)

type ErrorCode string

const (
	CodeSuccess   ErrorCode = "success"
	CodeGeneric   ErrorCode = "generic"
	CodeUsage     ErrorCode = "usage"
	CodeNotFound  ErrorCode = "not-found"
	CodeConflict  ErrorCode = "conflict"
	CodeIntegrity ErrorCode = "integrity"
)

const (
	ExitSuccess   = 0
	ExitGeneric   = 1
	ExitUsage     = 2
	ExitNotFound  = 3
	ExitConflict  = 4
	ExitIntegrity = 5
)

func CodeForError(err error) ErrorCode {
	if err == nil {
		return CodeSuccess
	}
	if errors.Is(err, ErrUsage) || errors.Is(err, store.ErrUsage) {
		return CodeUsage
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrNotFound) {
		return CodeNotFound
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, store.ErrConflict) {
		return CodeConflict
	}
	if errors.Is(err, store.ErrIntegrity) {
		return CodeIntegrity
	}
	return CodeGeneric
}

func ExitCodeForError(err error) int {
	switch CodeForError(err) {
	case CodeSuccess:
		return ExitSuccess
	case CodeUsage:
		return ExitUsage
	case CodeNotFound:
		return ExitNotFound
	case CodeConflict:
		return ExitConflict
	case CodeIntegrity:
		return ExitIntegrity
	default:
		return ExitGeneric
	}
}

var (
	ErrUsage    = errors.New("usage")
	ErrNotFound = errors.New("not-found")
	ErrConflict = errors.New("conflict")
)

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewErrorEnvelope(code ErrorCode, message string) ErrorEnvelope {
	return ErrorEnvelope{
		Error: ErrorBody{
			Code:    string(code),
			Message: message,
		},
	}
}

func NewErrorEnvelopeFromError(err error) ErrorEnvelope {
	return NewErrorEnvelope(CodeForError(err), err.Error())
}

func (e ErrorEnvelope) String() string {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf(`{"error":{"code":"generic","message":"marshal error"}}`)
	}
	return string(data)
}

func WriteErrorJSON(err error) int {
	env := NewErrorEnvelopeFromError(err)
	fmt.Fprintln(os.Stderr, env.String())
	return ExitCodeForError(err)
}
