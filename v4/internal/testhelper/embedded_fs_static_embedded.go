//go:build !melody_env_embedded && melody_static_embedded

package testhelper

import (
    "io/fs"
    "testing/fstest"
)

func NewEmbeddedEnvFs() fs.FS {
    return nil
}

/* NewEmbeddedStaticFs stands in for the filesystem a release build embeds and so holds the public directory the file server requires. */
func NewEmbeddedStaticFs() fs.FS {
    return fstest.MapFS{
        "public/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
    }
}
