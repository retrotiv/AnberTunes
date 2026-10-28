// Package assets embeds the fonts and artwork compiled into the binary.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed fonts/*.ttf logo.svg
var files embed.FS

// Fonts returns the bundled font files.
func Fonts() fs.FS {
	sub, err := fs.Sub(files, "fonts")
	if err != nil {
		panic(err)
	}
	return sub
}

// Logo returns the SVG source of the Boombrick mark.
func Logo() string {
	b, err := files.ReadFile("logo.svg")
	if err != nil {
		panic(err)
	}
	return string(b)
}
