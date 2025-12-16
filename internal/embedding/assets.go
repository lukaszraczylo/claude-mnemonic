// Package embedding provides text embedding generation using all-MiniLM-L6-v2.
package embedding

import (
	_ "embed"
)

// Model and tokenizer files - embedded for all platforms
//
//go:embed assets/model.onnx
var modelData []byte

//go:embed assets/tokenizer.json
var tokenizerData []byte
