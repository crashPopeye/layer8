package updater

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.10", "v0.1.9", true},
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0", "dev", false},
		{"garbage", "v0.1.0", false},
		{"v0.2.0-rc.1", "v0.1.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got, err := ChecksumFor(sum+"  layer8-0.2.0-macos-universal.zip\n"+sum[:63]+"0 *other.zip\n", "layer8-0.2.0-macos-universal.zip")
	if err != nil || got != sum {
		t.Fatal(got, err)
	}
	if _, err := ChecksumFor("", "x"); err == nil {
		t.Fatal("expected error")
	}
	if AssetName("v1.2.3") != "layer8-1.2.3-macos-universal.zip" {
		t.Fatal(AssetName("v1.2.3"))
	}
}

func TestParseTeamID(t *testing.T) {
	if ParseTeamID("Identifier=x\nTeamIdentifier=ABCDE12345\n") != "ABCDE12345" {
		t.Fatal("team")
	}
	if ParseTeamID("Signature=adhoc\nTeamIdentifier=not set\n") != "" {
		t.Fatal("adhoc")
	}
}

func TestBundlePathOutsideApp(t *testing.T) {
	if _, err := BundlePath(); err == nil {
		t.Fatal("test binary is not in a bundle")
	}
}
