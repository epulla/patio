package web

import "embed"

// Dist may contain only .gitkeep before frontend build.
//
//go:embed all:dist
var Dist embed.FS
