package task

import (
	"errors"
	"testing"
)

func TestTheme(t *testing.T) {
	svc := service(t)
	if got := svc.Theme(); got != "rose-pine" {
		t.Fatalf("default theme = %q", got)
	}
	if err := svc.SetTheme("solarized"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SetTheme(solarized) = %v", err)
	}
	if err := svc.SetTheme("dash"); err != nil || svc.Theme() != "dash" {
		t.Fatalf("SetTheme(dash) = %v, theme %q", err, svc.Theme())
	}
}
