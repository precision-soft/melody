package static

import (
    "io/fs"
)

type Mode string

const (
    ModeFilesystem Mode = "filesystem"
    ModeEmbedded   Mode = "embedded"
)

/* DefaultAllowedDotPrefix is the one dot-prefixed path element a file server retrieves out of the box: RFC 8615 publishes an ACME http-01 challenge, security.txt and assetlinks.json under it, and a deployment that renews its certificate through the application would otherwise lose that renewal to the dot-prefix refusal. */
const DefaultAllowedDotPrefix = ".well-known"

type FileServerConfig struct {
    mode        Mode
    publicDir   string
    indexFile   string
    stripPrefix string
    enableCache bool
    cacheMaxAge int
    weakEtag    bool

    allowedDotPrefixList []string
    excludedPathList     []string
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

        allowedDotPrefixList: []string{DefaultAllowedDotPrefix},
        excludedPathList:     []string{},
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

/* SetAllowedDotPrefixList permits selected dot-prefixed first path elements, defaulting to .well-known. Nested dot-prefixed elements remain refused. An empty list refuses all dot-prefixed paths. NewFileServer copies this option; later changes affect only subsequently constructed servers. */
func (instance *FileServerConfig) SetAllowedDotPrefixList(allowedDotPrefixList []string) {
    copied := []string{}
    if nil != allowedDotPrefixList {
        copied = append([]string{}, allowedDotPrefixList...)
    }

    instance.allowedDotPrefixList = copied
}

/* SetExcludedPathList makes matching path prefixes fall through without disk access. Matching uses routed path spelling before stripping or folding. An empty entry excludes every path; the default empty list excludes none. NewFileServer copies the option at construction. */
func (instance *FileServerConfig) SetExcludedPathList(excludedPathList []string) {
    copied := []string{}
    for _, excludedPath := range excludedPathList {

        if 0 < len(excludedPath) && '/' != excludedPath[0] {
            excludedPath = "/" + excludedPath
        }

        copied = append(copied, excludedPath)
    }

    instance.excludedPathList = copied
}
