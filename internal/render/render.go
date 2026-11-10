// Package render turns Markdown into sanitised HTML: GitHub-flavoured
// Markdown, an explicit allowlist of what may reach the page, and limits on
// size and rendering time.
package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// MaxDocSize is the largest Markdown file the viewer renders. Crafted input
// can make rendering take time that grows with the square of its size, so
// this stays well below what a real document needs.
const MaxDocSize = 2 << 20

// RenderTimeout bounds how long the viewer waits for one document.
const RenderTimeout = 10 * time.Second

// ErrTooSlow is returned when rendering takes longer than RenderTimeout.
var ErrTooSlow = errors.New("the document took too long to render")

// IsolateAbove is the size above which documents render in a separate
// process (see Renderer.Isolate). Below it the worst case is about a second
// of CPU, not worth a process start.
const IsolateAbove = 64 << 10

// Renderer turns Markdown into sanitised HTML. It is safe for concurrent use.
type Renderer struct {
	md      goldmark.Markdown
	policy  *bluemonday.Policy
	timeout time.Duration

	// Isolate, if set, renders documents larger than IsolateAbove in a
	// separate process that ends when ctx does. goldmark can't be
	// cancelled, so this is what stops a runaway render from using a CPU
	// core for minutes after the viewer gave up on it.
	Isolate func(ctx context.Context, src []byte) (string, error)

	mu   sync.Mutex
	slow map[[32]byte]bool // sources that timed out; refused right away

	slots chan struct{} // bounds renders running at once
}

// MaxRenders is how many documents render at the same time; a render waits
// for a slot (or for its request to be cancelled).
const MaxRenders = 2

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
	return &Renderer{md: md, policy: newPolicy(), timeout: RenderTimeout, slots: make(chan struct{}, MaxRenders)}
}

var (
	classNames = regexp.MustCompile(`^[\w\s-]+$`)
	alignments = regexp.MustCompile(`(?i)^(left|right|center)$`)
	checkbox   = regexp.MustCompile(`(?i)^checkbox$`)
)

// newPolicy is an explicit allowlist of what GitHub renders: bluemonday's
// UGC policy without image maps, plus <kbd>, task lists and the attributes
// highlighted code, footnotes and centred README headers use. Scripts, event
// handlers, styles, forms, iframes and image maps are dropped. An <area>
// click would navigate the page itself; the page must only ever show
// documents through Go.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowStandardAttributes() // dir, id, lang, title
	p.AllowStandardURLs()       // http, https, mailto and relative URLs only

	p.AllowElements("article", "aside", "figure", "figcaption", "section", "summary", "hgroup")
	p.AllowAttrs("open").Matching(regexp.MustCompile(`(?i)^(|open)$`)).OnElements("details")
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowElements("br", "div", "hr", "p", "span", "wbr")
	p.AllowAttrs("cite").OnElements("blockquote", "q")
	p.AllowAttrs("href").OnElements("a")
	p.AllowElements("abbr", "acronym", "cite", "code", "dfn", "em", "kbd", "mark", "s", "samp",
		"strong", "sub", "sup", "var", "b", "i", "pre", "small", "strike", "tt", "u", "del", "ins")
	p.AllowAttrs("datetime").Matching(bluemonday.ISO8601).OnElements("time", "del", "ins")
	p.AllowAttrs("dir").Matching(bluemonday.Direction).OnElements("bdi", "bdo")
	p.AllowElements("rp", "rt", "ruby")
	p.AllowLists()
	p.AllowTables()
	p.AllowImages() // img with src, alt, width, height; no usemap

	p.AllowAttrs("class").Matching(classNames).OnElements("pre", "code", "span", "div")
	p.AllowAttrs("align").Matching(alignments).OnElements("p", "div", "img", "h1", "h2", "h3", "h4", "h5", "h6", "td", "th")
	// GFM task lists.
	p.AllowAttrs("type").Matching(checkbox).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	// Footnote back-references point at ids in the same page.
	p.AllowAttrs("id").Matching(bluemonday.SpaceSeparatedTokens).OnElements("li", "sup", "a")
	return p
}

// Render converts src to sanitised HTML. It gives up when ctx is cancelled
// (returning ctx's error) or after the renderer's timeout (ErrTooSlow;
// that content is then refused at once). Large documents render in a
// separate process (Isolate), killed when the render is abandoned. Small
// ones render here: goldmark can't be cancelled, so an abandoned one runs
// on, about a second at most at that size, still holding its slot.
func (r *Renderer) Render(ctx context.Context, src []byte) (string, error) {
	sum := sha256.Sum256(src)
	r.mu.Lock()
	known := r.slow[sum]
	r.mu.Unlock()
	if known {
		return "", ErrTooSlow
	}
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if r.Isolate != nil && len(src) > IsolateAbove {
		defer func() { <-r.slots }()
		h, err := r.Isolate(tctx, src)
		return r.outcome(ctx, tctx, sum, h, err)
	}
	type result struct {
		html string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-r.slots }()
		h, err := r.render(src)
		done <- result{h, err}
	}()
	select {
	case res := <-done:
		return res.html, res.err
	case <-tctx.Done():
		return r.outcome(ctx, tctx, sum, "", tctx.Err())
	}
}

// outcome turns a render that ended into its result: cancelled, too slow,
// or what it produced.
func (r *Renderer) outcome(ctx, tctx context.Context, sum [32]byte, h string, err error) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if tctx.Err() != nil {
		r.markSlow(sum)
		return "", ErrTooSlow
	}
	return h, err
}

func (r *Renderer) markSlow(sum [32]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.slow == nil {
		r.slow = map[[32]byte]bool{}
	}
	r.slow[sum] = true
}

// RenderNow renders src in this goroutine, with no timeout: for the child
// process that Isolate starts.
func (r *Renderer) RenderNow(src []byte) (string, error) { return r.render(src) }

func (r *Renderer) render(src []byte) (string, error) {
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
