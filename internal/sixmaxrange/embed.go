package sixmaxrange

import _ "embed"

// defaultModel is deliberately unsupported. Release builds replace this
// file through a Go overlay with the fitted model kept under ignored bin/.
//
//go:embed default_model.json
var defaultModel []byte

func Embedded() (*Model, error) { return Decode(defaultModel) }
