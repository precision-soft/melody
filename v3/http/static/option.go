package static

import (
    "io/fs"
)

type Mode string

const (
    ModeFilesystem Mode = "filesystem"
    ModeEmbedded   Mode = "embedded"
)

/* DefaultAllowedDotPrefix is the one dot-prefixed element a file server retrieves by default: RFC 8615 publishes an ACME http-01 challenge, security.txt and assetlinks.json under it. */
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

/* SetAllowedDotPrefixList names the dot-prefixed first path elements the file server may retrieve; every other dot-prefixed element is refused, and the allowance never reaches past the first element. An empty list refuses every dot-prefixed path. NewFileServer copies the configuration, so this is set before the server is built. */
func (instance *FileServerConfig) SetAllowedDotPrefixList(allowedDotPrefixList []string) {
    copied := []string{}
    if nil != allowedDotPrefixList {
        copied = append([]string{}, allowedDotPrefixList...)
    }

    instance.allowedDotPrefixList = copied
}

/* SetExcludedPathList names the path prefixes the file server declines without looking at the disk, handing them down the chain to the application. An entry is a prefix of the request path as security.NewPathPrefixMatcher reads it, before the strip prefix and any fold, so a firewall rule and this list select the same requests; an empty entry switches the server off. The default excludes nothing. NewFileServer copies the configuration, so this is set before the server is built. */
func (instance *FileServerConfig) SetExcludedPathList(excludedPathList []string) {
    copied := []string{}
    for _, excludedPath := range excludedPathList {
        /* an entry without a leading slash could never match, so it is normalized; the configuration door refuses one */
        if 0 < len(excludedPath) && '/' != excludedPath[0] {
            excludedPath = "/" + excludedPath
        }

        copied = append(copied, excludedPath)
    }

    instance.excludedPathList = copied
}
