package completedfeed

import (
	"strings"
	"testing"
)

func TestCursorCodecRoundTripAndProjectBinding(t *testing.T) {
	codec, err := NewCursorCodec("cursor-secret", "dev-source")
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Encode(42, 99)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cursor, "42") || strings.Contains(cursor, "99") {
		t.Fatal("cursor must not expose a decimal sequence or project identity")
	}
	got, err := codec.Decode(cursor, 42)
	if err != nil || got != 99 {
		t.Fatalf("decode cursor: sequence=%d err=%v", got, err)
	}
	if _, err := codec.Decode(cursor, 43); err == nil {
		t.Fatal("cursor must be bound to its project")
	}
}

func TestCursorCodecRejectsTamperingAndSourceChange(t *testing.T) {
	codec, err := NewCursorCodec("cursor-secret", "dev-source")
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Encode(42, 99)
	if err != nil {
		t.Fatal(err)
	}
	last := cursor[len(cursor)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	if _, err := codec.Decode(cursor[:len(cursor)-1]+string(replacement), 42); err == nil {
		t.Fatal("tampered cursor must fail closed")
	}

	other, err := NewCursorCodec("cursor-secret", "other-source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decode(cursor, 42); err == nil {
		t.Fatal("cursor must be bound to source_instance")
	}
}
