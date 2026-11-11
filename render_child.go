package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/abelmatos/md-viewer/internal/render"
)

// renderFlag starts the exe as a render child: Markdown on stdin, sanitised
// HTML on stdout. The parent kills it when it takes too long.
const renderFlag = "-render"

// renderInChild renders src in a child copy of this exe, which ends when
// ctx does.
func renderInChild(ctx context.Context, src []byte) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, exe, renderFlag) //nolint:gosec // G204: our own executable
	cmd.Stdin = bytes.NewReader(src)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	hideChildWindow(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", fmt.Errorf("render process: %w", err)
	}
	return out.String(), nil
}

// renderChild is the child side.
func renderChild() int {
	src, err := io.ReadAll(io.LimitReader(os.Stdin, render.MaxDocSize+1))
	if err == nil && len(src) > render.MaxDocSize {
		err = errors.New("document too large")
	}
	var html string
	if err == nil {
		html, err = render.NewRenderer().RenderNow(src)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := io.WriteString(os.Stdout, html); err != nil {
		return 1
	}
	return 0
}
