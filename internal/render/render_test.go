package render

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	r := NewRenderer()
	src := "# Hello World\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n" +
		"- [x] done\n- [ ] todo\n\n" +
		"~~old~~ https://example.com\n\n" +
		"```go\nfunc main() {}\n```\n\n" +
		"<details><summary>More</summary>hidden</details>\n\n" +
		"<p align=\"center\"><img src=\"logo.png\" width=\"100\"></p>\n\n" +
		"Note[^1]\n\n[^1]: The note.\n"
	html, err := r.Render(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h1 id="hello-world">Hello World</h1>`,
		`<table>`,
		`<input checked="" disabled="" type="checkbox"`,
		`<del>old</del>`,
		`<a href="https://example.com"`,
		`class="chroma"`,
		`<details><summary>More</summary>`,
		`<p align="center"><img src="logo.png" width="100"></p>`,
		`id="fn:1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestRenderStripsScripts(t *testing.T) {
	r := NewRenderer()
	src := "<script>alert(1)</script>\n\n" +
		"<img src=x onerror=\"alert(2)\">\n\n" +
		"[click](javascript:alert(3))\n\n" +
		"<iframe src=\"https://evil.example\"></iframe>\n\n" +
		"<a href=\"#\" style=\"position:fixed\" onclick=\"alert(4)\">x</a>\n"
	html, err := r.Render(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "onerror", "javascript:", "<iframe", "onclick", "style="} {
		if strings.Contains(strings.ToLower(html), bad) {
			t.Errorf("%q survived sanitising:\n%s", bad, html)
		}
	}
}

func TestCodeCSS(t *testing.T) {
	css, err := CodeCSS()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(css, ".chroma") || !strings.Contains(css, "prefers-color-scheme: dark") {
		t.Errorf("unexpected CSS:\n%.300s", css)
	}
}

func TestRenderDropsImageMaps(t *testing.T) {
	html, err := NewRenderer().Render(context.Background(), []byte(`<img src="a.png" usemap="#m"><map name="m"><area shape="rect" coords="0,0,9,9" href="https://evil.example"></map>`+"\n\nPress <kbd>Ctrl</kbd>\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<map", "<area", "usemap", "evil.example"} {
		if strings.Contains(html, bad) {
			t.Errorf("%q survived:\n%s", bad, html)
		}
	}
	if !strings.Contains(html, "<kbd>Ctrl</kbd>") {
		t.Errorf("kbd dropped:\n%s", html)
	}
}

func TestRenderTimeout(t *testing.T) {
	r := NewRenderer()
	r.timeout = 50 * time.Millisecond
	// Unclosed links make goldmark's work grow with the square of the size.
	if _, err := r.Render(context.Background(), []byte(strings.Repeat("[a](", 40000))); !errors.Is(err, ErrTooSlow) {
		t.Errorf("err = %v, want ErrTooSlow", err)
	}
}
