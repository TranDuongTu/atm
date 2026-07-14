package eventsource

import (
	"bytes"
	"strings"
	"testing"
)

func TestMintReplicaIDShape(t *testing.T) {
	id, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0xA7}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "r_") || len(id) != 2+26 {
		t.Fatalf("id = %q, want r_ + 26 chars", id)
	}
	const crockford = "0123456789abcdefghjkmnpqrstvwxyz"
	for _, r := range id[2:] {
		if !strings.ContainsRune(crockford, r) {
			t.Fatalf("id %q contains non-Crockford char %q", id, r)
		}
	}
}

func TestMintReplicaIDIsDeterministicPerEntropy(t *testing.T) {
	a, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x43}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("same entropy, different ids: %s vs %s", a, b)
	}
	if a == c {
		t.Errorf("different entropy, same id: %s", a)
	}
}

func TestMintReplicaIDZeroEntropy(t *testing.T) {
	id, err := MintReplicaID(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if id != "r_"+strings.Repeat("0", 26) {
		t.Errorf("zero entropy id = %q", id)
	}
}

func TestMintReplicaIDShortEntropyFails(t *testing.T) {
	if _, err := MintReplicaID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("expected error for short entropy source")
	}
}

func TestReplicaV1IsReserved(t *testing.T) {
	if ReplicaV1 != "_v1" {
		t.Errorf("ReplicaV1 = %q", ReplicaV1)
	}
}
