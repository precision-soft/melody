//go:build !melody_static_embedded

package main

import "io/fs"

var embeddedPublicFiles fs.FS = nil
