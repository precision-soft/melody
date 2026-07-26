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

/* SetAllowedDotPrefixList names the dot-prefixed first path elements the file server may retrieve. The default carries ".well-known" alone, which is where an ACME http-01 challenge, security.txt and assetlinks.json are published, and every other dot-prefixed element stays refused so a stray .env or .git never leaves the public directory. The allowance never reaches past the first element, so ".well-known/.env" is refused exactly like "/.env". An empty list refuses every dot-prefixed path. */
func (instance *FileServerConfig) SetAllowedDotPrefixList(allowedDotPrefixList []string) {
    copied := []string{}
    if nil != allowedDotPrefixList {
        copied = append([]string{}, allowedDotPrefixList...)
    }

    instance.allowedDotPrefixList = copied
}

/* SetExcludedPathList names the path prefixes the file server declines without looking at the disk. A declined request continues down the rest of the chain, so an excluded prefix is how the part of the url it names is handed to the application: to a middleware that authenticates it, to a stricter policy, or to a file server of its own. An entry is a prefix of the request path exactly as security.NewPathPrefixMatcher reads one — the raw path, before the strip prefix is removed and before the path is folded — so a rule written for a firewall and a rule written here select the same requests. An empty entry therefore names every path and switches the file server off entirely. The default list is empty, which excludes nothing. */
func (instance *FileServerConfig) SetExcludedPathList(excludedPathList []string) {
    copied := []string{}
    if nil != excludedPathList {
        copied = append([]string{}, excludedPathList...)
    }

    instance.excludedPathList = copied
}
