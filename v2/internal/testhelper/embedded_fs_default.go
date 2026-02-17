//go:build !melody_env_embedded && !melody_static_embedded

package testhelper

import "io/fs"

func NewEmbeddedEnvFs() fs.FS {
	return nil
}

func NewEmbeddedStaticFs() fs.FS {
	return nil
}
