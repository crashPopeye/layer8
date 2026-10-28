// Package ui holds the frontend: plain HTML, CSS and JS served by the Go app.
// There is no build step; vendor/ holds the only third-party script.
package ui

import "embed"

//go:embed index.html styles.css js vendor fonts
var FS embed.FS
