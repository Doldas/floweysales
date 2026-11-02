package pluginsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Plugin wires handlers to a manifest. A nil handler answers "unsupported".
type Plugin struct {
	Manifest   Manifest
	Quote      func(context.Context, QuoteParams) (Asset, error)
	History    func(context.Context, HistoryParams) (HistorySeries, error)
	Search     func(context.Context, SearchParams) ([]SearchResult, error)
	Financials func(context.Context, FinancialsParams) (FinancialsResponse, error)
	Research   func(context.Context, ResearchParams) (ResearchResult, error)
	ForumPosts func(context.Context, ForumParams) ([]ForumPost, error)
	News       func(context.Context, NewsParams) ([]NewsItem, error)
	Events     func(context.Context, EventsParams) ([]CorporateEvent, error)
	// Tool runs a tool declared in Manifest.Tools.
	Tool func(context.Context, ToolParams) (ToolResult, error)
}

// maxRequestBytes bounds one request line. Market-data requests are tiny; a
// tool call carries the user's input files (MaxToolInputBytes) plus framing.
const maxRequestBytes = MaxToolInputBytes + 1<<20

// maxConcurrent bounds in-flight requests per process.
const maxConcurrent = 8

// Main is the entry point for a plugin binary: "--manifest" prints the
// manifest, otherwise the plugin serves the backend on stdin/stdout until
// stdin closes or the process is signalled.
func Main(p Plugin) {
	if err := p.Manifest.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "invalid plugin manifest:", err)
		os.Exit(2)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--manifest":
			_ = json.NewEncoder(os.Stdout).Encode(p.Manifest)
			return
		case "--version":
			fmt.Println(p.Manifest.Version)
			return
		default:
			fmt.Fprintf(os.Stderr, "%s %s is a FloweySales plugin; install it from Settings → Plugins.\nFlags: --manifest, --version\n", p.Manifest.Name, p.Manifest.Version)
			os.Exit(2)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := ServeIO(ctx, p, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ServeIO answers requests read from r on w until r is exhausted or ctx ends.
// Requests run concurrently; responses are written whole, one per line.
func ServeIO(ctx context.Context, p Plugin, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes)
	var (
		writeMu sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, maxConcurrent)
		enc     = json.NewEncoder(w)
	)
	send := func(resp Response) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := enc.Encode(resp); err != nil {
			// The result could not be encoded (for example NaN); report that instead.
			_ = enc.Encode(Response{ID: resp.ID, Error: &ResponseError{Code: CodeInternal, Message: "encode result: " + err.Error()}})
		}
	}
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			send(Response{Error: &ResponseError{Code: CodeInvalidArgument, Message: "malformed request"}})
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}
		wg.Add(1)
		go func(req Request) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if rec := recover(); rec != nil {
					send(Response{ID: req.ID, Error: &ResponseError{Code: CodeInternal, Message: fmt.Sprintf("plugin panic: %v", rec)}})
				}
			}()
			result, err := p.dispatch(ctx, req)
			if err != nil {
				var coded *ResponseError
				if !errors.As(err, &coded) {
					coded = &ResponseError{Code: CodeUpstream, Message: err.Error()}
				}
				send(Response{ID: req.ID, Error: coded})
				return
			}
			send(Response{ID: req.ID, Result: result})
		}(req)
	}
	wg.Wait()
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read request: %w", err)
	}
	return nil
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, Errorf(CodeInvalidArgument, "invalid parameters: %v", err)
	}
	return v, nil
}

func call[P, R any](ctx context.Context, raw json.RawMessage, fn func(context.Context, P) (R, error)) (any, error) {
	if fn == nil {
		return nil, Errorf(CodeUnsupported, "method not supported by this plugin")
	}
	params, err := decode[P](raw)
	if err != nil {
		return nil, err
	}
	return fn(ctx, params)
}

func (p Plugin) dispatch(ctx context.Context, req Request) (any, error) {
	switch req.Method {
	case MethodDescribe:
		return p.Manifest, nil
	case MethodQuote:
		return call(ctx, req.Params, p.Quote)
	case MethodHistory:
		return call(ctx, req.Params, p.History)
	case MethodSearch:
		return call(ctx, req.Params, p.Search)
	case MethodFinancials:
		return call(ctx, req.Params, p.Financials)
	case MethodResearch:
		return call(ctx, req.Params, p.Research)
	case MethodForumPosts:
		return call(ctx, req.Params, p.ForumPosts)
	case MethodNews:
		return call(ctx, req.Params, p.News)
	case MethodEvents:
		return call(ctx, req.Params, p.Events)
	case MethodTool:
		return p.runTool(ctx, req.Params)
	default:
		return nil, Errorf(CodeUnsupported, "unknown method %q", req.Method)
	}
}

// runTool answers a "tool" request for a tool the manifest declares and
// checks the result against the display contract.
func (p Plugin) runTool(ctx context.Context, raw json.RawMessage) (any, error) {
	if p.Tool == nil || p.Manifest.Tools == nil {
		return nil, Errorf(CodeUnsupported, "method not supported by this plugin")
	}
	params, err := decode[ToolParams](raw)
	if err != nil {
		return nil, err
	}
	if _, ok := p.Manifest.Tools.Tool(params.Tool); !ok {
		return nil, Errorf(CodeInvalidArgument, "unknown tool %q", params.Tool)
	}
	result, err := p.Tool(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, Errorf(CodeInternal, "invalid tool result: %v", err)
	}
	if result.Blocks == nil {
		result.Blocks = []ToolBlock{}
	}
	return result, nil
}
