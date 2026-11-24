package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func artifactURL(p string) string { return "/artifact/t1/key/" + p }

func TestGolden(t *testing.T) {
	for _, name := range []string{"table", "code", "mermaid", "artifact"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", name+".md"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Markdown(src, artifactURL)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", name+".golden.html")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("%s rendered\n%s\nwant\n%s", name, got, want)
			}
		})
	}
}

func TestArtifactSandbox(t *testing.T) {
	got, err := Markdown([]byte("```task-artifact\nmockup.html\n```\n"), artifactURL)
	if err != nil {
		t.Fatal(err)
	}
	html := string(got)
	if !strings.Contains(html, `sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"`) || strings.Contains(html, "allow-same-origin") {
		t.Fatalf("iframe sandbox: %s", html)
	}
	for _, escape := range []string{"../secret.html", "/etc/passwd"} {
		got, _ := Markdown([]byte("```task-artifact\n"+escape+"\n```\n"), artifactURL)
		if strings.Contains(string(got), "<iframe") || strings.Contains(string(got), "src=") {
			t.Fatalf("%s embedded: %s", escape, got)
		}
	}
}

func TestRawHTMLIsNotRendered(t *testing.T) {
	got, err := Markdown([]byte("hello <script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n"), artifactURL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "<script") || strings.Contains(string(got), "onerror") {
		t.Fatalf("raw HTML passed through: %s", got)
	}
}
