//go:build melody_env_embedded

package main

import (
	"embed"
	"io/fs"
)

//go:embed .env
var embeddedEnv embed.FS

var embeddedEnvFiles fs.FS = embeddedEnv
