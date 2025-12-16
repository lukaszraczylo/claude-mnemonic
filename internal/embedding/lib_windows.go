//go:build windows

package embedding

// Windows doesn't need the providers shared library
var onnxRuntimeProvidersLib []byte

const onnxRuntimeProvidersLibName = ""
