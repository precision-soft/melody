//go:build melody_env_embedded && melody_static_embedded

package testhelper

import (
	"io/fs"
	"testing/fstest"
)

func NewEmbeddedEnvFs() fs.FS {
	return fstest.MapFS{}
}

func NewEmbeddedStaticFs() fs.FS {
	return fstest.MapFS{}
}
