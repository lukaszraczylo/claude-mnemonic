// Package embedding provides text embedding generation using bge-small-en-v1.5.
package embedding

import (
	_ "embed"
)

// Tokenizer file - embedded for all platforms (small, not in LFS).
// The ONNX model is downloaded at runtime to ~/.claude-mnemonic/models/.
//
//go:embed assets/tokenizer.json
var tokenizerData []byte
