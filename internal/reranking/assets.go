// Package reranking provides cross-encoder reranking for search results.
package reranking

import (
	_ "embed"
)

// Tokenizer file - embedded for all platforms (small, not in LFS).
// The ONNX model is downloaded at runtime to ~/.claude-mnemonic/models/.
//
//go:embed assets/tokenizer.json
var crossEncoderTokenizerData []byte
