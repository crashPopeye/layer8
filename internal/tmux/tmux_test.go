package tmux

import (
	"context"
	"testing"
)

func TestQuoteAndName(t *testing.T) {
	if Quote("abc-1/x") != "abc-1/x" || Quote("it's") != `'it'\''s'` || Quote("") != "''" {
		t.Fatal("quote")
	}
	if Name("eng-1.2:x y") != "l8-eng-1-2-x-y" {
		t.Fatal(Name("eng-1.2:x y"))
	}
}

func TestSessionLifecycle(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	ctx := context.Background()
	name := Name("layer8-test-session")
	_ = Kill(ctx, name)
	if err := Ensure(ctx, name, t.TempDir(), map[string]string{"LAYER8_TASK": "x"}); err != nil {
		t.Fatal(err)
	}
	if !HasSession(ctx, name) {
		t.Fatal("session missing")
	}
	if err := Ensure(ctx, name, "/", nil); err != nil {
		t.Fatal("Ensure should be idempotent:", err)
	}
	if err := Kill(ctx, name); err != nil || HasSession(ctx, name) {
		t.Fatal("kill failed", err)
	}
}
