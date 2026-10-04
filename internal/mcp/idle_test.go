//go:build unix

package mcp

import (
	"bytes"
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingPipe returns the two ends of a pipe in blocking mode, the way an inherited stdin is: a Read on it cannot be
// woken by closing the file (the standard pipe from os.Pipe is pollable and can, which hid this).
func blockingPipe(t *testing.T) (r, w *os.File) {
	t.Helper()
	var fds [2]int
	require.NoError(t, syscall.Pipe(fds[:]))
	r = os.NewFile(uintptr(fds[0]), "stdin-like")
	w = os.NewFile(uintptr(fds[1]), "stdin-writer")
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	return r, w
}

type lockedBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The idle timeout used to close stdin and hope the read loop ended. On a real (blocking) stdin the read stays blocked,
// so the server logged "shutting down" and then lived on with a closed stdin; the next message from the client woke the
// read, which failed with "file already closed" and killed the connection mid-call (seen in Claude Desktop after the
// connector sat unused for more than 30 minutes). An idle timeout that is on must end Run for real.
func TestRun_IdleTimeoutEndsTheServerCleanly(t *testing.T) {
	t.Parallel()

	stdin, _ := blockingPipe(t)
	server := &Server{stdin: stdin, stdout: &lockedBuffer{}, idleTimeout: 50 * time.Millisecond, monitorInterval: 10 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()

	select {
	case err := <-done:
		require.NoError(t, err, "an idle shutdown is a clean exit, not an error")
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after the idle timeout: stdin was closed but the blocked read never ended")
	}
}

// By default there is no idle timeout: a stdio server lives as long as its client, and a client that talks to it after a
// long pause must get an answer.
func TestRun_WithoutAnIdleTimeoutTheServerKeepsAnsweringAfterALongPause(t *testing.T) {
	t.Parallel()

	stdin, stdinWriter := blockingPipe(t)
	stdout := &lockedBuffer{}
	server := &Server{stdin: stdin, stdout: stdout, monitorInterval: 5 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()

	time.Sleep(300 * time.Millisecond) // many monitor ticks with no traffic
	select {
	case err := <-done:
		t.Fatalf("Run ended during a pause with no traffic: %v", err)
	default:
	}

	_, err := stdinWriter.WriteString(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return bytes.Contains([]byte(stdout.String()), []byte(`"id":7`)) }, 3*time.Second, 10*time.Millisecond,
		"the request after the pause is answered")
	select {
	case err := <-done:
		t.Fatalf("Run ended after answering: %v", err)
	default:
	}
}
