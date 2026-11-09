// Command md-viewer is a small Windows viewer for Markdown files: it renders
// them like GitHub, lists every Markdown file in the folder, and follows links
// between them with back and forward.
package main

// Regenerate the Windows resources (icon, manifest, version info) after
// editing winres/.
//go:generate go tool go-winres make --arch amd64,arm64

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	debug := flag.Bool("debug", false, "enable the developer tools and context menu")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Usage: md-viewer [file.md | folder]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("md-viewer", version)
		return
	}
	os.Exit(run(flag.Arg(0), *debug))
}
