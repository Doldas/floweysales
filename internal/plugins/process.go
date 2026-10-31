package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxResponseBytes bounds one response line (a long OHLC history is a few MB).
const maxResponseBytes = 16 << 20

// CallError is an error a plugin reported for one call.
type CallError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *CallError) Error() string { return e.Message }

// Plugin error codes (see pluginsdk).
const (
	CodeUnsupported     = "unsupported"
	CodeInvalidArgument = "invalid_argument"
	CodeNotConfigured   = "not_configured"
	CodeUpstream        = "upstream"
	CodeInternal        = "internal"
)

// errProcessExited is returned for calls in flight when the plugin dies.
var errProcessExited = errors.New("plugin process exited")

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *CallError      `json:"error"`
}

// process is one running plugin. Calls are multiplexed by request id, so a
// slow history download does not block a quote.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tailBuffer

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan response
	done    chan struct{}
	exitErr error
}

// startProcess launches path with exactly env. The working directory is the
// binary's own directory.
func startProcess(path, dir string, env []string) (*process, error) {
	cmd := exec.Command(path)
	cmd.Dir = dir
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &process{cmd: cmd, stdin: stdin, stderr: newTailBuffer(40), pending: map[uint64]chan response{}, done: make(chan struct{})}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	go p.readLoop(stdout)
	return p, nil
}

func (p *process) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxResponseBytes)
	for scanner.Scan() {
		var resp response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			p.stderr.add("backend: discarded malformed plugin output")
			continue
		}
		p.mu.Lock()
		ch := p.pending[resp.ID]
		delete(p.pending, resp.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	err := scanner.Err()
	if err != nil {
		// An oversized or unreadable stream cannot be resynchronised.
		p.stderr.add("backend: " + err.Error())
		_ = p.cmd.Process.Kill()
	}
	waitErr := p.cmd.Wait()
	p.mu.Lock()
	p.exitErr = errProcessExited
	if waitErr != nil {
		p.exitErr = fmt.Errorf("%w: %v", errProcessExited, waitErr)
	}
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	p.mu.Unlock()
	close(p.done)
}

// alive reports whether the process is still running.
func (p *process) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// call sends one request and decodes its result into out (which may be nil).
func (p *process) call(ctx context.Context, method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan response, 1)
	p.mu.Lock()
	if p.exitErr != nil {
		err := p.exitErr
		p.mu.Unlock()
		return err
	}
	p.nextID++
	id := p.nextID
	p.pending[id] = ch
	p.mu.Unlock()
	forget := func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}

	line, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": json.RawMessage(raw)})
	p.writeMu.Lock()
	_, err = p.stdin.Write(append(line, '\n'))
	p.writeMu.Unlock()
	if err != nil {
		forget()
		return fmt.Errorf("write to plugin: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			p.mu.Lock()
			err := p.exitErr
			p.mu.Unlock()
			return err
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return &CallError{Code: CodeInternal, Message: "plugin returned an invalid " + method + " result"}
		}
		return nil
	case <-ctx.Done():
		forget()
		return ctx.Err()
	}
}

// stop closes stdin (the plugin's signal to exit) and kills it if it has not
// exited within the grace period.
func (p *process) stop(grace time.Duration) {
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

// tailBuffer keeps the last lines a plugin wrote to stderr for diagnostics.
type tailBuffer struct {
	mu      sync.Mutex
	max     int
	lines   []string
	partial strings.Builder
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range string(b) {
		if c == '\n' {
			t.push(t.partial.String())
			t.partial.Reset()
			continue
		}
		if t.partial.Len() < 500 {
			t.partial.WriteRune(c)
		}
	}
	return len(b), nil
}

func (t *tailBuffer) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.push(line)
}

func (t *tailBuffer) push(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tailBuffer) tail() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.lines...)
}
