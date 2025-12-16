//go:build darwin && arm64

package embedding

import (
	_ "embed"
)

//go:embed assets/lib/darwin-arm64/libonnxruntime.dylib
var onnxRuntimeLib []byte

const onnxRuntimeLibName = "libonnxruntime.dylib"
