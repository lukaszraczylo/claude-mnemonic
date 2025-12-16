//go:build darwin && amd64

package embedding

import (
	_ "embed"
)

//go:embed assets/lib/darwin-amd64/libonnxruntime.dylib
var onnxRuntimeLib []byte

const onnxRuntimeLibName = "libonnxruntime.dylib"
