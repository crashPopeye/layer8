package config

import (
	"os"
	"sync"
	"testing"
)

func TestTokenIsStableAndPrivate(t *testing.T) {
	t.Setenv("LAYER8_HOME", t.TempDir())
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	got := make([]string, 8)
	for i := range got {
		wg.Add(1)
		go func(i int) { defer wg.Done(); got[i], _ = Token(p) }(i)
	}
	wg.Wait()
	for _, g := range got {
		if g != got[0] || len(g) != 64 {
			t.Fatalf("tokens differ or malformed: %v", got)
		}
	}
	fi, _ := os.Stat(p.Token)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
}

func TestNormalize(t *testing.T) {
	t.Setenv("LAYER8_HOME", t.TempDir())
	p, _ := ResolvePaths()
	c := normalize(Config{Port: -1, MaxConcurrentSessions: 99, PermissionMode: "weird"}, p)
	if c.Port != 7878 || c.MaxConcurrentSessions != 32 || c.PermissionMode != "acceptEdits" {
		t.Fatalf("%+v", c)
	}
}
