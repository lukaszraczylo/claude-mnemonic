// Package reranking provides cross-encoder reranking for search results.
package reranking

import (
	_ "embed"
)

// Cross-encoder model and tokenizer files - embedded for all platforms
//
//go:embed assets/model.onnx
var crossEncoderModelData []byte

//go:embed assets/tokenizer.json
var crossEncoderTokenizerData []byte
