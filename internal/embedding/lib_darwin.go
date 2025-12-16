//go:build darwin

package embedding

// Darwin doesn't need the providers shared library
var onnxRuntimeProvidersLib []byte

const onnxRuntimeProvidersLibName = ""
