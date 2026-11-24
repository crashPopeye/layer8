// Package render turns Markdown artifacts into the HTML the artifacts panel shows.
package render

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Sandbox is the iframe sandbox of HTML artifacts: scripts and popups, never same-origin,
// so an artifact can't reach the daemon with the user's cookie.
const Sandbox = "allow-scripts allow-popups allow-popups-to-escape-sandbox"

// URLFunc maps a path inside the task's artifacts folder to the URL that serves it.
type URLFunc func(path string) string

// Markdown renders a Markdown artifact to HTML with GFM and chroma classes. Blocks carry
// data-line with their source line, ```mermaid fences become <pre class="mermaid">,
// ```task-artifact fences embed the file they name, and relative images load from the artifacts folder.
func Markdown(src []byte, artifact URLFunc) ([]byte, error) {
	body, skipped := stripFrontmatter(src)
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(
			util.Prioritized(annotate{lines: lineStarts(body), skipped: skipped, artifact: artifact}, 100),
		)),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(
			util.Prioritized(fences{artifact: artifact}, 100),
		)),
	)
	var out bytes.Buffer
	err := md.Convert(body, &out)
	return out.Bytes(), err
}

// stripFrontmatter drops a leading YAML block and returns how many lines it held.
func stripFrontmatter(src []byte) ([]byte, int) {
	if !bytes.HasPrefix(src, []byte("---\n")) {
		return src, 0
	}
	end := bytes.Index(src[4:], []byte("\n---\n"))
	if end < 0 {
		return src, 0
	}
	cut := 4 + end + len("\n---\n")
	return src[cut:], bytes.Count(src[:cut], []byte("\n"))
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// annotate sets data-line on every block and points relative images at the artifacts folder.
type annotate struct {
	lines    []int
	skipped  int
	artifact URLFunc
}

func (a annotate) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch {
		case n.Kind() == ast.KindImage:
			img := n.(*ast.Image)
			if p, ok := local(string(img.Destination)); ok {
				img.Destination = []byte(a.artifact(p))
			}
		case n.Type() == ast.TypeBlock && n.Kind() != ast.KindDocument:
			if offset, ok := firstOffset(n); ok {
				n.SetAttributeString("data-line", []byte(fmt.Sprint(a.line(offset))))
			}
		}
		return ast.WalkContinue, nil
	})
}

func (a annotate) line(offset int) int {
	return sort.Search(len(a.lines), func(i int) bool { return a.lines[i] > offset }) + a.skipped
}

// firstOffset is where a block starts in the source: its first line, or its first child's.
func firstOffset(n ast.Node) (int, bool) {
	if fence, ok := n.(*ast.FencedCodeBlock); ok && fence.Info != nil {
		return fence.Info.Segment.Start, true
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		return n.Lines().At(0).Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if offset, ok := firstOffset(c); ok {
			return offset, true
		}
	}
	return 0, false
}

// local cleans a reference to a file of the artifacts folder; it rejects URLs and paths that leave the folder.
func local(ref string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	p := path.Clean(strings.TrimPrefix(u.Path, ".layer8/task/"))
	return p, p != "." && filepath.IsLocal(filepath.FromSlash(p))
}

// fences renders fenced code blocks: Mermaid, embedded artifacts and highlighted code.
type fences struct{ artifact URLFunc }

func (f fences) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, f.render)
}

func (f fences) render(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	var info string
	if n.Info != nil {
		info = strings.TrimSpace(string(n.Info.Segment.Value(src)))
	}
	lang, arg, _ := strings.Cut(info, " ")
	var code strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		code.Write(line.Value(src))
	}
	switch lang {
	case "mermaid":
		w.WriteString(`<pre class="mermaid"`)
		gmhtml.RenderAttributes(w, n, nil)
		fmt.Fprintf(w, ">%s</pre>\n", html.EscapeString(code.String()))
	case "task-artifact":
		f.embed(w, n, strings.TrimSpace(arg+" "+code.String()))
	default:
		w.WriteString(`<div class="code"`)
		gmhtml.RenderAttributes(w, n, nil)
		w.WriteString(">")
		if err := highlight(w, lang, code.String()); err != nil {
			return ast.WalkStop, err
		}
		w.WriteString("</div>\n")
	}
	return ast.WalkSkipChildren, nil
}

// embed shows an artifact inline: HTML in a sandboxed iframe, images as images, anything else as a reference.
func (f fences) embed(w util.BufWriter, n ast.Node, ref string) {
	p, ok := local(ref)
	if !ok {
		fmt.Fprintf(w, "<p class=\"err\">task-artifact %s is outside the artifacts folder</p>\n", html.EscapeString(ref))
		return
	}
	src, name := html.EscapeString(f.artifact(p)), html.EscapeString(p)
	attrs := func() { gmhtml.RenderAttributes(w, n, nil) }
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm":
		fmt.Fprintf(w, `<iframe class="artifact" data-artifact="%s" sandbox="%s" src="%s" title="%s"`, name, Sandbox, src, name)
		attrs()
		w.WriteString("></iframe>\n")
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp":
		fmt.Fprintf(w, `<img class="artifact" data-artifact="%s" src="%s" alt="%s"`, name, src, name)
		attrs()
		w.WriteString(">\n")
	default:
		fmt.Fprintf(w, `<a class="artifact" data-artifact="%s" href="%s"`, name, src)
		attrs()
		fmt.Fprintf(w, ">%s</a>\n", name)
	}
}

var formatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.ClassPrefix("ch-"))

// highlight writes code as chroma-classed HTML; the stylesheet colours the classes.
func highlight(w util.BufWriter, lang, code string) error {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	tokens, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return err
	}
	return formatter.Format(w, styles.Fallback, tokens)
}
