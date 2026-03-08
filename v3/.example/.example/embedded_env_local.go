//go:build !melody_env_embedded

package main

import "io/fs"

var embeddedEnvFiles fs.FS = nil
