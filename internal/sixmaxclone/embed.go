package sixmaxclone

import _ "embed"

// defaultModel defers every prediction. Clone-enabled builds replace this
// file through a Go overlay with a fitted model kept under ignored bin/.
//
//go:embed default_model.json
var defaultModel []byte

func Embedded() (*Model, error) { return Decode(defaultModel) }
