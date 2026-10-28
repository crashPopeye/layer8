package shellenv

import "testing"

func TestExtract(t *testing.T) {
	cases := map[string]string{
		"motd junk\n" + marker + "/a:/b" + marker + "\nbye": "/a:/b",
		"no markers":               "",
		marker + "/only/start":     "",
		marker + "  /x  " + marker: "/x",
	}
	for in, want := range cases {
		if got := Extract(in); got != want {
			t.Errorf("Extract(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"/a", "", "/b", "/a"})
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("got %v", got)
	}
}
