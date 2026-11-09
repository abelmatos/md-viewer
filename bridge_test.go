package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testBridge runs posted functions on a goroutine standing in for the UI
// thread and records evaluated scripts.
type testBridge struct {
	*Bridge
	mu      sync.Mutex
	scripts []string
	ui      chan func()
}

func newTestBridge(t *testing.T) *testBridge {
	t.Helper()
	tb := &testBridge{ui: make(chan func(), 64)}
	b, err := NewBridge(func(f func()) { tb.ui <- f }, func(js string) {
		tb.mu.Lock()
		tb.scripts = append(tb.scripts, js)
		tb.mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	tb.Bridge = b
	done := make(chan struct{})
	go func() {
		for {
			select {
			case f := <-tb.ui:
				f()
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() { close(done) })
	return tb
}

func (tb *testBridge) send(t *testing.T, key string, id int, method string, params ...any) {
	t.Helper()
	raw := make([]json.RawMessage, len(params))
	for i, p := range params {
		raw[i], _ = json.Marshal(p)
	}
	msg, _ := json.Marshal(rpcRequest{Key: key, ID: id, Method: method, Params: raw})
	tb.Handle(string(msg))
}

// reply waits for the script answering id.
func (tb *testBridge) reply(t *testing.T, id int) string {
	t.Helper()
	want := "window.mdReply(" + itoa(id) + ","
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		tb.mu.Lock()
		for _, s := range tb.scripts {
			if strings.Contains(s, want) {
				tb.mu.Unlock()
				return s
			}
		}
		tb.mu.Unlock()
	}
	t.Fatalf("no reply to %d", id)
	return ""
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestBridgeKey(t *testing.T) {
	tb := newTestBridge(t)
	tb.register("ping", func([]json.RawMessage) (any, error) { return "pong", nil })
	tb.send(t, "wrong", 1, "ping")
	tb.send(t, "", 2, "ping")
	tb.Handle("not json")
	tb.send(t, tb.Key(), 3, "ping")
	got := tb.reply(t, 3)
	if !strings.Contains(got, `window.mdReply(3,true,"pong")`) {
		t.Errorf("reply = %s", got)
	}
	// Replies run only in a document holding the key.
	if !strings.HasPrefix(got, `if (window.__mdKey === "`+tb.Key()+`")`) {
		t.Errorf("reply not guarded: %s", got)
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	for _, s := range tb.scripts {
		if strings.Contains(s, "mdReply(1,") || strings.Contains(s, "mdReply(2,") {
			t.Errorf("answered a request without the key: %s", s)
		}
	}
}

func TestBridgeErrors(t *testing.T) {
	tb := newTestBridge(t)
	tb.register("boom", func([]json.RawMessage) (any, error) { panic("bad") })
	tb.register("fail", func([]json.RawMessage) (any, error) { return nil, errors.New(`no "way"`) })
	tb.register("two", func(p []json.RawMessage) (any, error) {
		var a string
		var b float64
		return nil, decode(p, &a, &b)
	})
	tb.send(t, tb.Key(), 1, "boom")
	tb.send(t, tb.Key(), 2, "fail")
	tb.send(t, tb.Key(), 3, "missing")
	tb.send(t, tb.Key(), 4, "two", "x")
	tb.send(t, tb.Key(), 5, "two", 1, 2)
	for id, want := range map[int]string{
		1: `false,"internal error"`,
		2: `false,"no \"way\""`,
		3: `false,"unknown method \"missing\""`,
		4: `false,"want 2 arguments, got 1"`,
		5: `false,"argument 1:`,
	} {
		if got := tb.reply(t, id); !strings.Contains(got, want) {
			t.Errorf("reply %d = %s, want %s", id, got, want)
		}
	}
}

func TestBridgeApp(t *testing.T) {
	root, p, a := setup(t)
	tb := newTestBridge(t)
	settings := settingsStore{path: filepath.Join(t.TempDir(), "s.json")}
	tb.RegisterApp(a, p, settings, root)

	tb.send(t, tb.Key(), 1, "mdStart")
	if got := tb.reply(t, 1); !strings.Contains(got, `"name":"README.md"`) {
		t.Fatalf("start = %.200s", got)
	}
	// A second start (the page reloaded) resumes instead of reopening.
	tb.send(t, tb.Key(), 2, "mdNavigate", "docs/guide.md", 0)
	tb.reply(t, 2)
	tb.send(t, tb.Key(), 3, "mdStart")
	if got := tb.reply(t, 3); !strings.Contains(got, `"name":"guide.md"`) {
		t.Fatalf("resume = %.200s", got)
	}
	// Dialogs run on the UI thread; cancelling returns null.
	p.pick = ""
	tb.send(t, tb.Key(), 4, "mdPickFolder")
	if got := tb.reply(t, 4); !strings.Contains(got, "mdReply(4,true,null)") {
		t.Errorf("cancelled pick = %s", got)
	}
	p.pick = filepath.Join(root, "notes")
	tb.send(t, tb.Key(), 5, "mdPickFolder")
	if got := tb.reply(t, 5); !strings.Contains(got, `"name":"a.md"`) {
		t.Errorf("pick = %.200s", got)
	}
	tb.send(t, tb.Key(), 6, "mdSaveSettings", Settings{SideWidth: 300})
	tb.reply(t, 6)
	tb.send(t, tb.Key(), 7, "mdSettings")
	if got := tb.reply(t, 7); !strings.Contains(got, `"sideWidth":300`) {
		t.Errorf("settings = %s", got)
	}
}
