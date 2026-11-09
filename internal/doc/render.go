// Package doc renders Markdown files and resolves what their links point to.
// It has no Windows dependencies, so it is tested on any platform.
package doc

import (
	"bytes"
	"fmt"
	"regexp"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Renderer turns Markdown into sanitised HTML. It is safe for concurrent use.
type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// NewRenderer returns a renderer for GitHub-flavoured Markdown (tables, task
// lists, strikethrough, autolinks, footnotes) with highlighted code blocks.
func NewRenderer() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		// Raw HTML is common in READMEs (<details>, <img>, centred headers).
		// It is let through here and sanitised below.
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	return &Renderer{md: md, policy: newPolicy()}
}

var (
	classNames = regexp.MustCompile(`^[\w\s-]+$`)
	alignments = regexp.MustCompile(`(?i)^(left|right|center)$`)
	checkbox   = regexp.MustCompile(`(?i)^checkbox$`)
)

// newPolicy allows what GitHub renders and drops scripts, event handlers,
// styles, forms and iframes.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(classNames).OnElements("pre", "code", "span", "div")
	p.AllowAttrs("align").Matching(alignments).OnElements("p", "div", "img", "h1", "h2", "h3", "h4", "h5", "h6", "td", "th")
	// GFM task lists.
	p.AllowAttrs("type").Matching(checkbox).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	// Footnote back-references point at ids in the same page.
	p.AllowAttrs("id").Matching(bluemonday.SpaceSeparatedTokens).OnElements("li", "sup", "a")
	return p
}

// Render converts src to sanitised HTML.
func (r *Renderer) Render(src []byte) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(src, &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return r.policy.Sanitize(buf.String()), nil
}

// CodeCSS returns the stylesheet for highlighted code: GitHub colours in
// light mode and GitHub Dark when Windows is in dark mode.
func CodeCSS() (string, error) {
	f := chromahtml.New(chromahtml.WithClasses(true))
	var light, dark bytes.Buffer
	if err := f.WriteCSS(&light, styles.Get("github")); err != nil {
		return "", err
	}
	if err := f.WriteCSS(&dark, styles.Get("github-dark")); err != nil {
		return "", err
	}
	return light.String() + "\n@media (prefers-color-scheme: dark) {\n" + dark.String() + "}\n", nil
}
