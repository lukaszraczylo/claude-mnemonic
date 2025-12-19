// Package embedding provides text embedding generation with swappable models.
package embedding

import (
	"fmt"
	"sync"
)

// PoolingStrategy defines how to pool token embeddings into sentence embeddings.
type PoolingStrategy string

const (
	// PoolingNone means the model already outputs sentence embeddings directly.
	PoolingNone PoolingStrategy = "none"
	// PoolingMean averages all token embeddings (weighted by attention mask).
	PoolingMean PoolingStrategy = "mean"
	// PoolingCLS uses only the [CLS] token embedding.
	PoolingCLS PoolingStrategy = "cls"
)

// ONNXConfig describes ONNX-specific model configuration.
// This allows different models to specify their tensor names and pooling needs.
type ONNXConfig struct {
	// InputNames are the ONNX input tensor names in order.
	InputNames []string
	// OutputNames are the ONNX output tensor names.
	OutputNames []string
	// Pooling specifies how to convert token embeddings to sentence embeddings.
	// If PoolingNone, the model outputs sentence embeddings directly.
	Pooling PoolingStrategy
	// HiddenSize is the embedding dimension (used for pooling calculations).
	HiddenSize int
}

// EmbeddingModel represents a text embedding model.
type EmbeddingModel interface {
	// Name returns the human-readable model name (e.g., "bge-small-en-v1.5").
	Name() string

	// Version returns a short version string for storage (e.g., "bge-v1.5").
	Version() string

	// Dimensions returns the embedding vector size.
	Dimensions() int

	// Embed generates an embedding for a single text.
	Embed(text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(texts []string) ([][]float32, error)

	// Close releases model resources.
	Close() error
}

// ONNXConfigurer is an optional interface that models can implement
// to expose their ONNX configuration for introspection.
type ONNXConfigurer interface {
	// ONNXConfig returns the model's ONNX configuration.
	ONNXConfig() ONNXConfig
}

// ModelMetadata describes an embedding model for UI/config.
type ModelMetadata struct {
	Name        string `json:"name"`        // Human-readable name
	Version     string `json:"version"`     // Short ID for DB storage
	Dimensions  int    `json:"dimensions"`  // Vector size
	Description string `json:"description"` // Brief description
	Default     bool   `json:"default"`     // Is this the default model?
}

// ModelFactory creates a new instance of an embedding model.
type ModelFactory func() (EmbeddingModel, error)

// ModelRegistry provides model lookup by version.
type ModelRegistry struct {
	mu           sync.RWMutex
	models       map[string]ModelFactory
	metadata     map[string]ModelMetadata
	defaultModel string
}

// NewModelRegistry creates a new model registry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		models:   make(map[string]ModelFactory),
		metadata: make(map[string]ModelMetadata),
	}
}

// Register adds a model factory to the registry.
func (r *ModelRegistry) Register(meta ModelMetadata, factory ModelFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.models[meta.Version] = factory
	r.metadata[meta.Version] = meta

	if meta.Default {
		r.defaultModel = meta.Version
	}
}

// Get creates a new instance of the model with the given version.
func (r *ModelRegistry) Get(version string) (EmbeddingModel, error) {
	r.mu.RLock()
	factory, ok := r.models[version]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown model version: %s", version)
	}

	return factory()
}

// Default returns the default model version.
func (r *ModelRegistry) Default() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultModel
}

// List returns metadata for all registered models.
func (r *ModelRegistry) List() []ModelMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ModelMetadata, 0, len(r.metadata))
	for _, meta := range r.metadata {
		result = append(result, meta)
	}
	return result
}

// DefaultRegistry is the global model registry with all available models.
var DefaultRegistry = NewModelRegistry()

// RegisterModel adds a model to the default registry.
func RegisterModel(meta ModelMetadata, factory ModelFactory) {
	DefaultRegistry.Register(meta, factory)
}

// GetModel creates a model instance from the default registry.
func GetModel(version string) (EmbeddingModel, error) {
	return DefaultRegistry.Get(version)
}

// GetDefaultModel returns the default model version from the default registry.
func GetDefaultModel() string {
	return DefaultRegistry.Default()
}

// ListModels returns metadata for all models in the default registry.
func ListModels() []ModelMetadata {
	return DefaultRegistry.List()
}
