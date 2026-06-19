package embedding

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddingDim verifies the embedding dimension constant.
func TestEmbeddingDim(t *testing.T) {
	assert.Equal(t, 384, EmbeddingDim)
}

// TestNewService tests creating a new embedding service.
func TestNewService(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	require.NotNil(t, svc)

	defer svc.Close()

	// Verify the service is properly initialized via public methods
	assert.NotEmpty(t, svc.Name())
	assert.NotEmpty(t, svc.Version())
	assert.Equal(t, EmbeddingDim, svc.Dimensions())
}

// TestEmbed_SingleText tests embedding a single text.
func TestEmbed_SingleText(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	embedding, err := svc.Embed("Hello, world!")
	require.NoError(t, err)

	assert.Len(t, embedding, EmbeddingDim)

	// Verify non-zero embedding
	var sum float32
	for _, v := range embedding {
		sum += v * v
	}
	assert.Greater(t, sum, float32(0), "Embedding should not be all zeros")
}

// TestEmbed_EmptyText tests embedding an empty string.
func TestEmbed_EmptyText(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	embedding, err := svc.Embed("")
	require.NoError(t, err)

	assert.Len(t, embedding, EmbeddingDim)

	// Empty text should return zero vector
	for _, v := range embedding {
		assert.Equal(t, float32(0), v)
	}
}

// TestEmbed_SimilarTexts tests that similar texts produce similar embeddings.
func TestEmbed_SimilarTexts(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	emb1, err := svc.Embed("The quick brown fox jumps over the lazy dog.")
	require.NoError(t, err)

	emb2, err := svc.Embed("A fast brown fox leaps over a sleepy dog.")
	require.NoError(t, err)

	emb3, err := svc.Embed("Go programming language concurrency patterns.")
	require.NoError(t, err)

	// Calculate cosine similarity
	sim12 := cosineSimilarity(emb1, emb2)
	sim13 := cosineSimilarity(emb1, emb3)

	// Similar texts should have higher similarity
	assert.Greater(t, sim12, sim13, "Similar sentences should have higher similarity than dissimilar ones")
	assert.Greater(t, sim12, float64(0.7), "Similar sentences should have high similarity")
}

// TestEmbedBatch_MultipleTexts tests batch embedding.
func TestEmbedBatch_MultipleTexts(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	texts := []string{
		"First text about programming.",
		"Second text about databases.",
		"Third text about machine learning.",
	}

	embeddings, err := svc.EmbedBatch(texts)
	require.NoError(t, err)

	assert.Len(t, embeddings, len(texts))
	for i, emb := range embeddings {
		assert.Len(t, emb, EmbeddingDim, "Embedding %d should have correct dimension", i)
	}
}

// TestEmbedBatch_EmptySlice tests batch embedding with empty slice.
func TestEmbedBatch_EmptySlice(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	embeddings, err := svc.EmbedBatch([]string{})
	require.NoError(t, err)
	assert.Nil(t, embeddings)
}

// TestEmbedBatch_WithEmptyTexts tests batch embedding with some empty texts.
func TestEmbedBatch_WithEmptyTexts(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	texts := []string{
		"Valid text one.",
		"",
		"Valid text two.",
		"",
	}

	embeddings, err := svc.EmbedBatch(texts)
	require.NoError(t, err)

	assert.Len(t, embeddings, 4)

	// Non-empty texts should have non-zero embeddings
	var sum0 float32
	for _, v := range embeddings[0] {
		sum0 += v * v
	}
	assert.Greater(t, sum0, float32(0))

	// Empty texts should have zero embeddings
	for _, v := range embeddings[1] {
		assert.Equal(t, float32(0), v)
	}

	var sum2 float32
	for _, v := range embeddings[2] {
		sum2 += v * v
	}
	assert.Greater(t, sum2, float32(0))

	for _, v := range embeddings[3] {
		assert.Equal(t, float32(0), v)
	}
}

// TestEmbedBatch_AllEmpty tests batch embedding with all empty texts.
func TestEmbedBatch_AllEmpty(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	texts := []string{"", "", ""}

	embeddings, err := svc.EmbedBatch(texts)
	require.NoError(t, err)

	assert.Len(t, embeddings, 3)
	for i, emb := range embeddings {
		assert.Len(t, emb, EmbeddingDim, "Embedding %d should have correct dimension", i)
		for j, v := range emb {
			assert.Equal(t, float32(0), v, "Embedding %d[%d] should be zero", i, j)
		}
	}
}

// TestEmbed_Concurrent tests concurrent embedding calls.
func TestEmbed_Concurrent(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	var wg sync.WaitGroup
	numGoroutines := 10

	errors := make(chan error, numGoroutines)
	embeddings := make([][]float32, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			text := "Test text for concurrent embedding test"
			emb, err := svc.Embed(text)
			if err != nil {
				errors <- err
				return
			}
			embeddings[idx] = emb
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent embedding error: %v", err)
	}

	// All embeddings should be valid
	for i, emb := range embeddings {
		if emb != nil {
			assert.Len(t, emb, EmbeddingDim, "Embedding %d should have correct dimension", i)
		}
	}
}

// TestEmbed_SpecialCharacters tests embedding text with special characters.
func TestEmbed_SpecialCharacters(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	texts := []string{
		"Text with unicode: 你好世界 🎉",
		"Text with newlines:\nLine 1\nLine 2",
		"Text with tabs:\tColumn1\tColumn2",
		"Text with quotes: \"quoted\" and 'single'",
		"Text with code: func main() { fmt.Println(\"hello\") }",
	}

	for _, text := range texts {
		t.Run(text[:20], func(t *testing.T) {
			emb, err := svc.Embed(text)
			require.NoError(t, err)
			assert.Len(t, emb, EmbeddingDim)
		})
	}
}

// TestEmbed_LongText tests embedding long text.
func TestEmbed_LongText(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	// Create a long text (tokenizer should truncate appropriately)
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "This is a sentence to make the text very long. "
	}

	emb, err := svc.Embed(longText)
	require.NoError(t, err)
	assert.Len(t, emb, EmbeddingDim)
}

// TestClose tests closing the service.
func TestClose(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)

	err = svc.Close()
	require.NoError(t, err)

	// After close, embedding should fail (model resources released)
	// Note: This behavior is model-specific; some models may still work after close
}

// TestEmbedBatch_SingleItem tests batch embedding with single item.
func TestEmbedBatch_SingleItem(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	texts := []string{"Single text for batch embedding."}

	embeddings, err := svc.EmbedBatch(texts)
	require.NoError(t, err)

	assert.Len(t, embeddings, 1)
	assert.Len(t, embeddings[0], EmbeddingDim)
}

// TestEmbed_Deterministic tests that embedding is deterministic.
func TestEmbed_Deterministic(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	text := "Test text for deterministic embedding."

	emb1, err := svc.Embed(text)
	require.NoError(t, err)

	emb2, err := svc.Embed(text)
	require.NoError(t, err)

	// Same text should produce same embedding
	for i := 0; i < EmbeddingDim; i++ {
		assert.Equal(t, emb1[i], emb2[i], "Embedding should be deterministic at index %d", i)
	}
}

// --- Regression tests: context-aware mutex (Fix #45) ---

// asBGE casts the service model to *bgeModel for direct mutex access.
// Tests are in the same package so this is safe.
func asBGE(t *testing.T, svc *Service) *bgeModel {
	t.Helper()
	m, ok := svc.model.(*bgeModel)
	require.True(t, ok, "model is not *bgeModel — test invariant broken")
	return m
}

// holdMutex locks m.mu in a background goroutine and returns a release func.
// The returned ready channel is closed once the lock is held.
func holdMutex(m *bgeModel) (ready <-chan struct{}, release func()) {
	ch := make(chan struct{})
	done := make(chan struct{})
	go func() {
		m.mu.Lock()
		close(ch) // signal: lock acquired
		<-done    // wait for release signal
		m.mu.Unlock()
	}()
	return ch, func() { close(done) }
}

// TestEmbedWithContext_CancelWhileWaitingForMutex is the core regression test.
// If the mutex is held and the context times out, EmbedWithContext must return
// immediately with a context error — not block until the mutex is released.
func TestEmbedWithContext_CancelWhileWaitingForMutex(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	m := asBGE(t, svc)

	// Hold the mutex to simulate a stuck ONNX call.
	ready, release := holdMutex(m)
	<-ready // ensure lock is held before proceeding
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = svc.EmbedWithContext(ctx, "test text")
	elapsed := time.Since(start)

	// Must return a context error.
	require.Error(t, err)
	assert.True(t,
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"expected context error, got: %v", err)

	// Must return quickly (well under the 30 s default; allow 2× the timeout for CI slack).
	assert.Less(t, elapsed, 200*time.Millisecond,
		"EmbedWithContext blocked too long: %v", elapsed)
}

// TestEmbedWithContext_SuccessWhenUncontended verifies normal operation still works.
func TestEmbedWithContext_SuccessWhenUncontended(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	emb, err := svc.EmbedWithContext(context.Background(), "hello world")
	require.NoError(t, err)

	assert.Len(t, emb, EmbeddingDim)

	var sum float32
	for _, v := range emb {
		sum += v * v
	}
	assert.Greater(t, sum, float32(0), "embedding should not be all zeros")
}

// TestEmbedBatchWithContext_CancelDuringBatch verifies batch embedding respects
// context cancellation while blocked on mutex acquisition.
func TestEmbedBatchWithContext_CancelDuringBatch(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	m := asBGE(t, svc)

	ready, release := holdMutex(m)
	<-ready
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = svc.EmbedBatchWithContext(ctx, []string{"a", "b", "c"})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t,
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"expected context error, got: %v", err)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"EmbedBatchWithContext blocked too long: %v", elapsed)
}

// TestEmbedWithContext_CleanupAfterCancel verifies the cleanup goroutine in
// acquireMutex properly unlocks the mutex after context cancellation,
// so subsequent calls do not deadlock.
func TestEmbedWithContext_CleanupAfterCancel(t *testing.T) {
	svc, err := NewService()
	require.NoError(t, err)
	defer svc.Close()

	m := asBGE(t, svc)

	// --- first call: context expires while mutex is held ---
	ready, release := holdMutex(m)
	<-ready

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, firstErr := svc.EmbedWithContext(ctx, "should fail")
	require.Error(t, firstErr)
	assert.True(t,
		errors.Is(firstErr, context.DeadlineExceeded) || errors.Is(firstErr, context.Canceled),
		"expected context error on first call, got: %v", firstErr)

	// Release the held mutex so the cleanup goroutine inside acquireMutex can finish.
	release()

	// Give the cleanup goroutine a moment to acquire-and-release the mutex.
	// 50 ms is generous; the goroutine only has to lock+unlock with no contention.
	time.Sleep(50 * time.Millisecond)

	// --- second call: mutex should be free, no deadlock ---
	emb, secondErr := svc.EmbedWithContext(context.Background(), "should work")
	require.NoError(t, secondErr, "second call should succeed after cleanup goroutine released mutex")
	assert.Len(t, emb, EmbeddingDim)
}

// Helper function to calculate cosine similarity
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct float64
	var normA float64
	var normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
