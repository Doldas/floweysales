package plugins

import "testing"

// alive must not report a process whose exit is already recorded, even while
// its done channel is still open (the window readLoop leaves between the two).
func TestAliveFalseOnceExitRecorded(t *testing.T) {
	p := &process{pending: map[uint64]chan response{}, done: make(chan struct{})}
	if !p.alive() {
		t.Fatal("a fresh process is alive")
	}
	p.mu.Lock()
	p.exitErr = errProcessExited
	p.mu.Unlock()
	if p.alive() {
		t.Fatal("process with a recorded exit reported alive")
	}
}
