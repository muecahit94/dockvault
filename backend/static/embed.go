package static

import "embed"

// DistFS holds the compiled React frontend assets when built into the binary.
//
//go:embed all:dist
var DistFS embed.FS
