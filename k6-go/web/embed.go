// Package web holds the static frontend, embedded into the server binary.
package web

import "embed"

//go:embed index.html
var Files embed.FS
