//go:build melody_static_embedded

package main

import (
	"embed"
	"io/fs"
)

//go:embed public public
var embeddedStatic embed.FS

var embeddedPublicFiles fs.FS = embeddedStatic
