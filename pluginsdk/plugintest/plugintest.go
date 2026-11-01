// Package plugintest checks that a plugin behaves the way the FloweySales
// backend relies on, through the real wire protocol. Use it from a plugin's
// own tests:
//
//	func TestConformance(t *testing.T) { plugintest.Conformance(t, newPlugin()) }
package plugintest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

// Client drives a Plugin over the wire protocol in-process.
type Client struct {
	in     *io.PipeWriter
	mu     sync.Mutex
	nextID uint64
	waits  map[uint64]chan response
	done   chan struct{}
}

type response struct {
	ID     uint64             `json:"id"`
	Result json.RawMessage    `json:"result"`
	Error  *sdk.ResponseError `json:"error"`
}

// TB is the part of testing.TB the checks use.
type TB interface {
	Helper()
	Cleanup(func())
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

var _ TB = (testing.TB)(nil)

// Start serves p on pipes until the test ends.
func Start(t TB, p sdk.Plugin) *Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &Client{in: inW, waits: map[uint64]chan response{}, done: make(chan struct{})}
	go func() {
		_ = sdk.ServeIO(context.Background(), p, inR, outW)
		_ = outW.Close()
	}()
	go func() {
		defer close(c.done)
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 64*1024), 16<<20)
		for scanner.Scan() {
			var r response
			if json.Unmarshal(scanner.Bytes(), &r) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.waits[r.ID]
			delete(c.waits, r.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- r
			}
		}
	}()
	t.Cleanup(func() { _ = inW.Close(); <-c.done })
	return c
}

// Call sends one request and decodes the result into out (may be nil). A
// plugin error is returned as *sdk.ResponseError.
func (c *Client) Call(method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan response, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.waits[id] = ch
	c.mu.Unlock()
	line, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": json.RawMessage(raw)})
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return r.Error
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-time.After(30 * time.Second):
		return errors.New("plugin did not answer within 30s")
	}
}

// methods maps each capability to its method and whether p implements it.
func methods(p sdk.Plugin) map[string]struct {
	method string
	has    bool
} {
	return map[string]struct {
		method string
		has    bool
	}{
		sdk.CapQuote:      {sdk.MethodQuote, p.Quote != nil},
		sdk.CapHistory:    {sdk.MethodHistory, p.History != nil},
		sdk.CapSearch:     {sdk.MethodSearch, p.Search != nil},
		sdk.CapFinancials: {sdk.MethodFinancials, p.Financials != nil},
		sdk.CapResearch:   {sdk.MethodResearch, p.Research != nil},
		sdk.CapForum:      {sdk.MethodForumPosts, p.ForumPosts != nil},
		sdk.CapNews:       {sdk.MethodNews, p.News != nil},
		sdk.CapEvents:     {sdk.MethodEvents, p.Events != nil},
	}
}

// Conformance checks, without network access, that:
//   - the manifest is valid for this SDK's protocol;
//   - "describe" returns exactly the manifest;
//   - every declared capability has a handler and every handler is declared
//     (the backend never calls an undeclared one);
//   - undeclared capabilities and research operations answer "unsupported".
func Conformance(t *testing.T, p sdk.Plugin) {
	t.Helper()
	conformance(t, p)
}

func conformance(t TB, p sdk.Plugin) {
	t.Helper()
	if err := p.Manifest.Validate(); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	c := Start(t, p)
	var described sdk.Manifest
	if err := c.Call(sdk.MethodDescribe, struct{}{}, &described); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !reflect.DeepEqual(normalize(described), normalize(p.Manifest)) {
		t.Errorf("describe does not return the manifest:\n got %+v\nwant %+v", described, p.Manifest)
	}
	for capability, m := range methods(p) {
		declared := p.Manifest.Provider.HasCapability(capability)
		switch {
		case declared && !m.has:
			t.Errorf("capability %q is declared but %s has no handler", capability, m.method)
		case !declared && m.has:
			t.Errorf("%s has a handler but capability %q is not declared, so FloweySales never calls it", m.method, capability)
		case !declared:
			if code := errorCode(c.Call(m.method, map[string]any{}, nil)); code != sdk.CodeUnsupported {
				t.Errorf("undeclared %s answered %q, want %q", m.method, code, sdk.CodeUnsupported)
			}
		}
	}
	hasTools := p.Manifest.Tools != nil
	switch {
	case hasTools && p.Tool == nil:
		t.Errorf("tools are declared but %s has no handler", sdk.MethodTool)
	case !hasTools && p.Tool != nil:
		t.Errorf("%s has a handler but no tools are declared, so FloweySales never calls it", sdk.MethodTool)
	case !hasTools:
		if code := errorCode(c.Call(sdk.MethodTool, map[string]any{}, nil)); code != sdk.CodeUnsupported {
			t.Errorf("undeclared %s answered %q, want %q", sdk.MethodTool, code, sdk.CodeUnsupported)
		}
	default:
		if code := errorCode(c.Call(sdk.MethodTool, sdk.ToolParams{Tool: "not_declared_by_this_plugin"}, nil)); code != sdk.CodeInvalidArgument {
			t.Errorf("an undeclared tool answered %q, want %q", code, sdk.CodeInvalidArgument)
		}
	}
	if p.Manifest.Provider.HasCapability(sdk.CapResearch) && p.Research != nil {
		err := c.Call(sdk.MethodResearch, sdk.ResearchParams{Operation: "not_declared_by_this_plugin", Symbol: "TEST"}, nil)
		if code := errorCode(err); code != sdk.CodeUnsupported {
			t.Errorf("an undeclared research operation answered %q (%v), want %q", code, err, sdk.CodeUnsupported)
		}
	}
}

func errorCode(err error) string {
	var coded *sdk.ResponseError
	if errors.As(err, &coded) {
		return coded.Code
	}
	if err == nil {
		return "success"
	}
	return fmt.Sprintf("transport error: %v", err)
}

// normalize compares manifests by their JSON form (nil vs empty slices).
func normalize(m sdk.Manifest) map[string]any {
	raw, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
