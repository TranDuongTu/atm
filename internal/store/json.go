package store

import (
	"atm/internal/core"
	"atm/internal/store/fsio"
)

// Delegations kept so the store's many callers stay unchanged; the
// implementations live in core (pure marshaling) and fsio (file I/O).
var (
	MarshalSorted    = core.MarshalSorted
	WriteFileAtomic  = fsio.WriteFileAtomic
	WriteBytesAtomic = fsio.WriteBytesAtomic
	WriteJSON        = fsio.WriteJSON
	ReadJSON         = fsio.ReadJSON
)
