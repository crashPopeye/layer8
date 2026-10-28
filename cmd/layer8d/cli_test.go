package main

import (
	"reflect"
	"testing"
)

func TestFlags(t *testing.T) {
	o, rest := flags([]string{"log", "-f", "abc"}, "f")
	if o["f"] != "1" || !reflect.DeepEqual(rest, []string{"log", "abc"}) {
		t.Fatal(o, rest)
	}
	o, rest = flags([]string{"T-1", "-s", "pr2", "do", "it"}, "s")
	if o["s"] != "pr2" || !reflect.DeepEqual(rest, []string{"T-1", "do", "it"}) {
		t.Fatal(o, rest)
	}
}
