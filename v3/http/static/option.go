package static

import (
    "io/fs"
)

type Mode string

const (
    ModeFilesystem Mode = "filesystem"
    ModeEmbedded   Mode = "embedded"
)

type FileServerConfig struct {
    mode        Mode
    publicDir   string
    indexFile   string
    stripPrefix string
    enableCache bool
    cacheMaxAge int /** in seconds */
    weakEtag    bool
}

func NewFileServerConfig(
    mode Mode,
    publicDir string,
    indexFile string,
    stripPrefix string,
    enableCache bool,
    cacheMaxAge int,
    weakEtag bool,
) *FileServerConfig {
    return &FileServerConfig{
        mode:        mode,
        publicDir:   publicDir,
        indexFile:   indexFile,
        stripPrefix: stripPrefix,
        enableCache: enableCache,
        cacheMaxAge: cacheMaxAge,
        weakEtag:    weakEtag,
    }
}

type Options struct {
    fileServerConfig *FileServerConfig
    root             string
    fileSystem       fs.FS
}

func NewOptions(
    fileServerConfig *FileServerConfig,
    root string,
    fileSystem fs.FS,
) *Options {
    return &Options{
        fileServerConfig: fileServerConfig,
        root:             root,
        fileSystem:       fileSystem,
    }
}
