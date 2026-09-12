//go:build !melody_env_embedded && melody_static_embedded

package testhelper

import (
    "io/fs"
    "testing/fstest"
)

func NewEmbeddedEnvFs() fs.FS {
    return nil
}

/* NewEmbeddedStaticFs stands in for the filesystem a release build embeds, and therefore holds the public directory: an embedded filesystem without one is the wiring fault the file server refuses at construction, so an empty stand-in tests a shape no build produces. */
func NewEmbeddedStaticFs() fs.FS {
    return fstest.MapFS{
        "public/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
    }
}
