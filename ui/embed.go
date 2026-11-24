// Package ui holds the frontend: plain HTML, CSS and ES modules with no build step.
package ui

import "embed"

//go:embed index.html styles.css js vendor
var FS embed.FS
