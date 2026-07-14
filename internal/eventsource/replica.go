package eventsource

import (
	"fmt"
	"io"
	"math/big"
)

// ReplicaV1 is the reserved replica id used exclusively by the D6 upgrade
// so that every machine upgrading the same v1 log derives byte-identical
// events. It MUST never be minted for a live replica (L0-3).
const ReplicaV1 = "_v1"

// crockford32 is Crockford base32, lowercase (no i, l, o, u).
const crockford32 = "0123456789abcdefghjkmnpqrstvwxyz"

// MintReplicaID mints a fresh replica id — "r_" + 26 Crockford base32
// characters encoding 128 bits read from r (callers pass crypto/rand's
// Reader; tests pass fixed bytes). A replica id is local state: embedded
// in authored events, never itself synced as content.
func MintReplicaID(r io.Reader) (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return "", fmt.Errorf("eventsource: mint replica id: %w", err)
	}
	n := new(big.Int).SetBytes(buf[:])
	base := big.NewInt(32)
	mod := new(big.Int)
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		n.DivMod(n, base, mod)
		out[i] = crockford32[mod.Int64()]
	}
	return "r_" + string(out), nil
}
