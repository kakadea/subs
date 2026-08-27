package webassets

import "embed"

// Files are embedded so the production container does not depend on a host path.
//
//go:embed templates/*.html static/*
var Files embed.FS
