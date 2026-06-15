// Package web holds the embedded server-rendered dashboard assets: the HTML
// templates and the static CSS/JS served alongside them. Embedding keeps the
// dashboard a pure-Go, single-binary deploy with no runtime file dependencies.
package web

import "embed"

// Templates holds the dashboard HTML templates: a base layout, one template per
// page, and the small row partials that htmx swaps in place.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds the dashboard's static assets (CSS, and later a vendored htmx).
// Serve it with http.FileServerFS under a /static/ prefix.
//
//go:embed static
var Static embed.FS
