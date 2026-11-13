//go:build melody_env_embedded && !melody_static_embedded

package testhelper

import (
    "io/fs"
    "testing/fstest"
)

func NewEmbeddedEnvironmentFs() fs.FS {
    return fstest.MapFS{
        ".env": &fstest.MapFile{Data: []byte("MELODY_ENV=dev\n")},
    }
}

func NewEmbeddedStaticFs() fs.FS {
    return nil
}
