package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"sync/atomic"
)

// The bridge is the trust boundary between the page and Go.
//
// The page sends requests with chrome.webview.postMessage:
//
//	{"key": "<per-launch secret>", "session": "<per-load id>", "id": 7, "method": "mdNavigate", "params": ["docs/a.md", 120]}
//
// Go runs the method and answers by evaluating, in the page,
//
//	if (window.__mdKey === "<key>" && window.__mdSession === "<session>") window.mdReply(7, true, <result JSON>)
//
// The key is generated at launch and exists only inside the app's own page
// (see shellHTML), so a request from any other document is dropped and an
// answer never runs in one. The session is a random id the page picks each
// time it loads: when the app's page is put back, request ids start at 1
// again, and the session keeps a late answer to the previous load from
// resolving the new load's request. Methods receive only JSON values and
// check them like any other untrusted input.
//
// Every method runs on its own goroutine, so file access and rendering never
// freeze the window. What must happen on the UI thread (the open dialogs,
// window calls) is handed to it with post.

// rpcRequest is one call from the page.
type rpcRequest struct {
	Key     string            `json:"key"`
	Session string            `json:"session"`
	ID      int               `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type handler func(params []json.RawMessage) (any, error)

// Bridge dispatches page requests to Go methods.
type Bridge struct {
	key     string
	post    func(func()) // runs f on the UI thread after the current event handler
	eval    func(string) // runs a script in the page; UI thread only
	methods map[string]handler
}

// NewBridge returns a bridge with a fresh key. post and eval are the
// window's.
func NewBridge(post func(func()), eval func(string)) (*Bridge, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Bridge{
		key:     base64.RawURLEncoding.EncodeToString(b),
		post:    post,
		eval:    eval,
		methods: map[string]handler{},
	}, nil
}

// Key is the secret the page must send with every request.
func (b *Bridge) Key() string { return b.key }

// Handle runs one message from the page. It is called on the UI thread.
func (b *Bridge) Handle(msg string) {
	var req rpcRequest
	if err := json.Unmarshal([]byte(msg), &req); err != nil {
		log.Printf("bridge: dropped malformed message")
		return
	}
	if req.Key != b.key {
		log.Printf("bridge: dropped %q without the page key", req.Method)
		return
	}
	// The session is written into the reply's script.
	if !sessionID.MatchString(req.Session) {
		log.Printf("bridge: dropped %q with a malformed session", req.Method)
		return
	}
	h, ok := b.methods[req.Method]
	if !ok {
		b.reply(req.Session, req.ID, nil, fmt.Errorf("unknown method %q", req.Method))
		return
	}
	go func() {
		res, err := safeCall(h, req.Params)
		b.reply(req.Session, req.ID, res, err)
	}()
}

// sessionID is what the page's per-load session id must look like.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// safeCall runs h, turning a panic into an error so one bad request can't
// take the app down.
func safeCall(h handler, params []json.RawMessage) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("bridge: panic: %v", r)
			err = errors.New("internal error")
		}
	}()
	return h(params)
}

// reply sends a result or error to the page load that asked.
func (b *Bridge) reply(session string, id int, res any, err error) {
	ok, value := true, res
	if err != nil {
		ok, value = false, err.Error()
	}
	data, mErr := json.Marshal(value)
	if mErr != nil {
		ok, data = false, []byte(strconv.Quote(mErr.Error()))
	}
	b.post(func() {
		b.eval(b.guard(" && window.__mdSession === "+strconv.Quote(session),
			"window.mdReply("+strconv.Itoa(id)+","+strconv.FormatBool(ok)+","+string(data)+")"))
	})
}

// Notify shows an error in the page, for failures that happen outside a
// request (e.g. the browser didn't open a link). Safe from any goroutine.
func (b *Bridge) Notify(err error) {
	data, _ := json.Marshal(err.Error())
	b.post(func() { b.eval(b.guard("", "window.mdFail("+string(data)+")")) })
}

// guard makes script run only in the app's own page, and only where cond
// (a further "&& …" test, or "") holds too.
func (b *Bridge) guard(cond, script string) string {
	return "if (window.__mdKey === " + strconv.Quote(b.key) + cond + ") { " + script + " }"
}

// register adds a method. Register all methods before the page loads.
func (b *Bridge) register(name string, h handler) {
	b.methods[name] = h
}

// onUI runs f on the UI thread and waits for it.
func (b *Bridge) onUI(f func()) {
	done := make(chan struct{})
	b.post(func() { defer close(done); f() })
	<-done
}

// decode unmarshals params into dst, which must match them in number.
func decode(params []json.RawMessage, dst ...any) error {
	if len(params) != len(dst) {
		return fmt.Errorf("want %d arguments, got %d", len(dst), len(params))
	}
	for i, d := range dst {
		if err := json.Unmarshal(params[i], d); err != nil {
			return fmt.Errorf("argument %d: %w", i+1, err)
		}
	}
	return nil
}

// RegisterApp adds the viewer's methods. initial is the file or folder from
// the command line, shown by the first mdStart; later calls (after the page
// reloaded) resume the current state.
func (b *Bridge) RegisterApp(app *App, platform Platform, settings *settingsStore, initial string) {
	var started atomic.Bool
	b.register("mdStart", func(p []json.RawMessage) (any, error) {
		if err := decode(p); err != nil {
			return nil, err
		}
		if started.CompareAndSwap(false, true) && initial != "" {
			return app.Open(initial)
		}
		return app.Resume()
	})
	withScroll := func(f func(float64) (*View, error)) handler {
		return func(p []json.RawMessage) (any, error) {
			var scroll float64
			if err := decode(p, &scroll); err != nil {
				return nil, err
			}
			return f(scroll)
		}
	}
	b.register("mdBack", withScroll(app.Back))
	b.register("mdForward", withScroll(app.Forward))
	b.register("mdReload", withScroll(app.Reload))
	b.register("mdNavigate", func(p []json.RawMessage) (any, error) {
		var href string
		var scroll float64
		if err := decode(p, &href, &scroll); err != nil {
			return nil, err
		}
		return app.Navigate(href, scroll)
	})
	b.register("mdShow", func(p []json.RawMessage) (any, error) {
		var path string
		var scroll float64
		if err := decode(p, &path, &scroll); err != nil {
			return nil, err
		}
		return app.Show(path, scroll)
	})
	b.register("mdImage", func(p []json.RawMessage) (any, error) {
		var docPath, src string
		if err := decode(p, &docPath, &src); err != nil {
			return nil, err
		}
		return app.Image(docPath, src)
	})
	// The dialog runs on the UI thread; opening what was picked doesn't.
	pick := func(dialog func() (string, error)) handler {
		return func(p []json.RawMessage) (any, error) {
			if err := decode(p); err != nil {
				return nil, err
			}
			var path string
			var err error
			b.onUI(func() { path, err = dialog() })
			if err != nil || path == "" {
				return nil, err
			}
			return app.Open(path)
		}
	}
	b.register("mdPickFile", pick(platform.PickFile))
	b.register("mdPickFolder", pick(platform.PickFolder))
	b.register("mdTheme", func(p []json.RawMessage) (any, error) {
		var t Theme
		if err := decode(p, &t); err != nil {
			return nil, err
		}
		platform.SetTheme(t)
		return nil, nil
	})
	b.register("mdSettings", func(p []json.RawMessage) (any, error) {
		if err := decode(p); err != nil {
			return nil, err
		}
		return settings.Load(), nil
	})
	b.register("mdSaveSettings", func(p []json.RawMessage) (any, error) {
		var s Settings
		if err := decode(p, &s); err != nil {
			return nil, err
		}
		return nil, settings.Save(s)
	})
}
