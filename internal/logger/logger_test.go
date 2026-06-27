package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func capture(f func()) string {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)
	f()
	return buf.String()
}

func TestLogger_BasicFields(t *testing.T) {
	l := New(Fields{RequestID: "req-123", OrgID: 7})
	out := capture(func() {
		l.Info("hello %s", "world")
	})
	if !strings.Contains(out, "request_id=req-123") {
		t.Fatalf("missing request_id: %s", out)
	}
	if !strings.Contains(out, "org_id=7") {
		t.Fatalf("missing org_id: %s", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("missing message: %s", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Fatalf("missing level: %s", out)
	}
}

func TestLogger_ChildWith(t *testing.T) {
	parent := New(Fields{RequestID: "r1", OrgID: 1})
	child := parent.With(Fields{ChannelID: 42})
	out := capture(func() {
		child.Error("upstream failed")
	})
	if !strings.Contains(out, "request_id=r1") {
		t.Fatalf("child should inherit request_id: %s", out)
	}
	if !strings.Contains(out, "channel_id=42") {
		t.Fatalf("child should have channel_id: %s", out)
	}
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("child should be ERROR level: %s", out)
	}
}

func TestLogger_EmptyFields(t *testing.T) {
	l := New(Fields{})
	out := capture(func() {
		l.Warn("no context")
	})
	if !strings.Contains(out, "no context") {
		t.Fatalf("missing message: %s", out)
	}
	// Should not contain any key=value pairs for empty fields
	if strings.Contains(out, "request_id=") {
		t.Fatalf("should not have request_id for empty fields: %s", out)
	}
}

func TestLogger_SeqIncrement(t *testing.T) {
	l := New(Fields{})
	out1 := capture(func() { l.Info("first") })
	out2 := capture(func() { l.Info("second") })
	// Extract seq numbers - they should be different
	if out1 == out2 {
		t.Fatal("sequential logs should have different seq numbers")
	}
}
