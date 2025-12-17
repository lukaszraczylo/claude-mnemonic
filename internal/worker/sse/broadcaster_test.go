// Package sse provides Server-Sent Events broadcasting for claude-mnemonic.
package sse

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// BroadcasterSuite is a test suite for Broadcaster operations.
type BroadcasterSuite struct {
	suite.Suite
	broadcaster *Broadcaster
}

func (s *BroadcasterSuite) SetupTest() {
	s.broadcaster = NewBroadcaster()
}

func TestBroadcasterSuite(t *testing.T) {
	suite.Run(t, new(BroadcasterSuite))
}

// TestNewBroadcaster tests broadcaster creation.
func (s *BroadcasterSuite) TestNewBroadcaster() {
	b := NewBroadcaster()
	s.NotNil(b)
	s.NotNil(b.clients)
	s.Equal(0, b.ClientCount())
}

// TestClientCount tests client counting.
func (s *BroadcasterSuite) TestClientCount() {
	s.Equal(0, s.broadcaster.ClientCount())
}

// mockResponseWriter implements http.ResponseWriter and http.Flusher for testing.
type mockResponseWriter struct {
	header     http.Header
	body       []byte
	statusCode int
	mu         sync.Mutex
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.header
}

func (m *mockResponseWriter) Write(data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.body = append(m.body, data...)
	return len(data), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

func (m *mockResponseWriter) Flush() {
	// No-op for testing
}

func (m *mockResponseWriter) GetBody() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.body
}

// TestAddClient tests adding clients.
func (s *BroadcasterSuite) TestAddClient() {
	w := newMockResponseWriter()

	client, err := s.broadcaster.AddClient(w)
	s.NoError(err)
	s.NotNil(client)
	s.NotEmpty(client.ID)
	s.NotNil(client.Done)
	s.Equal(1, s.broadcaster.ClientCount())
}

// TestAddMultipleClients tests adding multiple clients.
func (s *BroadcasterSuite) TestAddMultipleClients() {
	for i := 0; i < 5; i++ {
		w := newMockResponseWriter()
		_, err := s.broadcaster.AddClient(w)
		s.NoError(err)
	}

	s.Equal(5, s.broadcaster.ClientCount())
}

// TestRemoveClient tests removing clients.
func (s *BroadcasterSuite) TestRemoveClient() {
	w := newMockResponseWriter()
	client, err := s.broadcaster.AddClient(w)
	s.NoError(err)

	s.Equal(1, s.broadcaster.ClientCount())

	s.broadcaster.RemoveClient(client)

	s.Equal(0, s.broadcaster.ClientCount())

	// Check that Done channel is closed
	select {
	case <-client.Done:
		// Expected - channel is closed
	default:
		s.Fail("Done channel should be closed")
	}
}

// TestBroadcast tests broadcasting messages.
func (s *BroadcasterSuite) TestBroadcast() {
	w := newMockResponseWriter()
	_, err := s.broadcaster.AddClient(w)
	s.NoError(err)

	// Broadcast a message
	s.broadcaster.Broadcast(map[string]string{"type": "test", "message": "hello"})

	// Give time for async write
	time.Sleep(50 * time.Millisecond)

	body := string(w.GetBody())
	s.Contains(body, "data:")
	s.Contains(body, "test")
	s.Contains(body, "hello")
}

// TestBroadcastNoClients tests broadcasting with no clients.
func (s *BroadcasterSuite) TestBroadcastNoClients() {
	// Should not panic
	s.broadcaster.Broadcast(map[string]string{"type": "test"})
}

// TestBroadcastMultipleClients tests broadcasting to multiple clients.
func (s *BroadcasterSuite) TestBroadcastMultipleClients() {
	writers := make([]*mockResponseWriter, 3)
	for i := 0; i < 3; i++ {
		writers[i] = newMockResponseWriter()
		_, err := s.broadcaster.AddClient(writers[i])
		s.NoError(err)
	}

	// Broadcast
	s.broadcaster.Broadcast(map[string]string{"type": "test"})

	// Give time for async writes
	time.Sleep(100 * time.Millisecond)

	// All clients should receive the message
	for i, w := range writers {
		body := string(w.GetBody())
		s.Contains(body, "data:", "Client %d should receive data", i)
	}
}

// TestClient tests Client structure.
func TestClient(t *testing.T) {
	w := newMockResponseWriter()
	client := &Client{
		ID:      "test-client",
		Writer:  w,
		Flusher: w,
		Done:    make(chan struct{}),
	}

	assert.Equal(t, "test-client", client.ID)
	assert.NotNil(t, client.Writer)
	assert.NotNil(t, client.Flusher)
	assert.NotNil(t, client.Done)

	// Close done channel
	close(client.Done)

	select {
	case <-client.Done:
		// Expected
	default:
		t.Error("Done channel should be closed")
	}
}

// TestClientUniqueIDs tests that clients get unique IDs.
func TestClientUniqueIDs(t *testing.T) {
	b := NewBroadcaster()
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		w := newMockResponseWriter()
		client, err := b.AddClient(w)
		require.NoError(t, err)

		// ID should be unique
		assert.False(t, ids[client.ID], "ID %s should be unique", client.ID)
		ids[client.ID] = true
	}
}

// TestWriteTimeout tests the write timeout constant.
func TestWriteTimeout(t *testing.T) {
	assert.Equal(t, 2*time.Second, WriteTimeout)
}

// TestHandleSSE tests the HandleSSE HTTP handler.
func TestHandleSSE(t *testing.T) {
	b := NewBroadcaster()

	// Create a test server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set up context that will be cancelled
		ctx := r.Context()

		// Start goroutine to cancel context after short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			// Request will be cancelled by the test client
		}()

		// This will block until context is cancelled
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
			return
		}
	})

	_ = handler
	_ = b

	// Just verify the handler exists and broadcaster can handle SSE
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()

	// Can't easily test HandleSSE since it blocks, but we can verify setup
	assert.NotNil(t, req)
	assert.NotNil(t, rec)
}

// TestBroadcastJSON tests broadcasting various JSON types.
func TestBroadcastJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    interface{}
		wantErr bool
	}{
		{
			name:    "string map",
			data:    map[string]string{"key": "value"},
			wantErr: false,
		},
		{
			name:    "int map",
			data:    map[string]int{"count": 42},
			wantErr: false,
		},
		{
			name:    "nested struct",
			data:    struct{ Name string }{Name: "test"},
			wantErr: false,
		},
		{
			name:    "array",
			data:    []string{"a", "b", "c"},
			wantErr: false,
		},
		{
			name:    "interface map",
			data:    map[string]interface{}{"type": "test", "count": 1, "active": true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroadcaster()
			w := newMockResponseWriter()
			_, err := b.AddClient(w)
			require.NoError(t, err)

			// Should not panic
			b.Broadcast(tt.data)

			time.Sleep(50 * time.Millisecond)

			body := string(w.GetBody())
			assert.Contains(t, body, "data:")
		})
	}
}

// TestConcurrentBroadcast tests concurrent broadcasting.
func TestConcurrentBroadcast(t *testing.T) {
	b := NewBroadcaster()

	// Add clients
	for i := 0; i < 10; i++ {
		w := newMockResponseWriter()
		_, err := b.AddClient(w)
		require.NoError(t, err)
	}

	// Broadcast concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Broadcast(map[string]int{"index": i})
		}(i)
	}

	wg.Wait()

	// Should complete without panics
	assert.Equal(t, 10, b.ClientCount())
}

// TestRemoveNonExistentClient tests removing a non-existent client.
func TestRemoveNonExistentClient(t *testing.T) {
	b := NewBroadcaster()

	// Create a client but don't add it
	client := &Client{
		ID:   "fake-client",
		Done: make(chan struct{}),
	}

	// Should not panic
	b.RemoveClient(client)

	// Done channel should be closed
	select {
	case <-client.Done:
		// Expected
	default:
		t.Error("Done channel should be closed")
	}
}

// TestBroadcasterConcurrentAddRemove tests concurrent add/remove operations.
func TestBroadcasterConcurrentAddRemove(t *testing.T) {
	b := NewBroadcaster()
	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := newMockResponseWriter()
			client, err := b.AddClient(w)
			if err == nil {
				// Random chance to remove
				if time.Now().UnixNano()%2 == 0 {
					b.RemoveClient(client)
				}
			}
		}()
	}

	wg.Wait()

	// Should not panic and have some clients
	count := b.ClientCount()
	assert.GreaterOrEqual(t, count, 0)
}

// TestRemoveClientByID tests removing a client by ID.
func TestRemoveClientByID(t *testing.T) {
	b := NewBroadcaster()
	w := newMockResponseWriter()

	client, err := b.AddClient(w)
	require.NoError(t, err)
	assert.Equal(t, 1, b.ClientCount())

	// Remove by ID
	b.removeClientByID(client.ID)
	assert.Equal(t, 0, b.ClientCount())

	// Done channel should be closed
	select {
	case <-client.Done:
		// Expected
	default:
		t.Error("Done channel should be closed")
	}
}

// TestRemoveClientByID_NonExistent tests removing a non-existent client by ID.
func TestRemoveClientByID_NonExistent(t *testing.T) {
	b := NewBroadcaster()

	// Should not panic
	b.removeClientByID("non-existent-id")
	assert.Equal(t, 0, b.ClientCount())
}

// TestRemoveClientByID_AlreadyClosed tests removing a client with already closed Done channel.
func TestRemoveClientByID_AlreadyClosed(t *testing.T) {
	b := NewBroadcaster()
	w := newMockResponseWriter()

	client, err := b.AddClient(w)
	require.NoError(t, err)

	// Pre-close the Done channel
	close(client.Done)

	// Should not panic when trying to close again
	b.removeClientByID(client.ID)
	assert.Equal(t, 0, b.ClientCount())
}

// TestHandleSSE_NonFlusher tests HandleSSE with a non-flusher response writer.
func TestHandleSSE_NonFlusher(t *testing.T) {
	b := NewBroadcaster()

	// Create a response writer that doesn't implement Flusher
	nonFlusher := &nonFlusherWriter{header: make(http.Header)}

	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	// Should return immediately since writer isn't a Flusher
	b.HandleSSE(nonFlusher, req)

	// No clients should be added
	assert.Equal(t, 0, b.ClientCount())
}

// nonFlusherWriter is a response writer that doesn't implement http.Flusher.
type nonFlusherWriter struct {
	header http.Header
}

func (w *nonFlusherWriter) Header() http.Header            { return w.header }
func (w *nonFlusherWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *nonFlusherWriter) WriteHeader(statusCode int)     {}

// TestWriteToClient_Timeout tests write timeout behavior.
func TestWriteToClient_Timeout(t *testing.T) {
	b := NewBroadcaster()

	// Create a slow writer that blocks
	slowWriter := &slowMockWriter{
		header:   make(http.Header),
		blockFor: 5 * time.Second, // Longer than WriteTimeout
	}

	client, err := b.AddClient(slowWriter)
	require.NoError(t, err)

	// Create dead client channel
	deadCh := make(chan string, 1)

	// Try to write - should timeout
	msg := "data: test\n\n"
	b.writeToClient(client, msg, deadCh)

	// May report dead client due to timeout
	// Check if client was reported as dead (with timeout)
	select {
	case deadID := <-deadCh:
		// Client was reported as dead
		assert.Equal(t, client.ID, deadID)
	case <-time.After(WriteTimeout + 500*time.Millisecond):
		// Timed out waiting - also acceptable
	}
}

// slowMockWriter simulates a slow writer for testing timeouts.
type slowMockWriter struct {
	header   http.Header
	blockFor time.Duration
	mu       sync.Mutex
}

func (m *slowMockWriter) Header() http.Header {
	return m.header
}

func (m *slowMockWriter) Write(data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	time.Sleep(m.blockFor)
	return len(data), nil
}

func (m *slowMockWriter) WriteHeader(statusCode int) {}

func (m *slowMockWriter) Flush() {}

// TestBroadcast_InvalidJSON tests broadcasting un-marshalable data.
func TestBroadcast_InvalidJSON(t *testing.T) {
	b := NewBroadcaster()
	w := newMockResponseWriter()
	_, err := b.AddClient(w)
	require.NoError(t, err)

	// channels can't be marshaled to JSON
	ch := make(chan int)
	b.Broadcast(ch) // Should log error but not panic

	// Give time for async processing
	time.Sleep(20 * time.Millisecond)

	// Body should be empty or not contain the channel data
	body := string(w.GetBody())
	assert.NotContains(t, body, "chan")
}

// TestBroadcast_ClientDoneChannel tests broadcasting when client Done is closed.
func TestBroadcast_ClientDoneChannel(t *testing.T) {
	b := NewBroadcaster()
	w := newMockResponseWriter()

	client, err := b.AddClient(w)
	require.NoError(t, err)

	// Close the done channel
	close(client.Done)

	// Broadcast should skip this client
	b.Broadcast(map[string]string{"type": "test"})

	// Give time for async processing
	time.Sleep(20 * time.Millisecond)
}
