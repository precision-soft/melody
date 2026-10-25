package static

import (
    "io/fs"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type Mode string

const (
    ModeFilesystem Mode = "filesystem"
    ModeEmbedded   Mode = "embedded"
)

/* DefaultAllowedDotPrefix is the one dot-prefixed element a file server retrieves by default: RFC 8615 publishes an ACME http-01 challenge, security.txt and assetlinks.json under it. */
const DefaultAllowedDotPrefix = ".well-known"

/* Config is what a file server serves and how. A field left out takes its default: the filesystem mode, the "public" directory under the root ".", "index.html", no strip prefix, Cache-Control with a 3600s max-age and strong entity tags, ".well-known" the one dot-prefixed element retrieved, no path excluded. NewFileServer copies it, lists included, so the server is immutable once built. */
type Config struct {
    /* Mode is filesystem or embedded; "" reads as filesystem. */
    Mode Mode

    /* PublicDir is the directory served, relative to Root in the filesystem mode and inside FileSystem in the embedded mode; "" reads as "public" in the filesystem mode and as the filesystem's root in the embedded mode. */
    PublicDir string

    /* IndexFile answers a directory request; "" reads as "index.html". */
    IndexFile string

    StripPrefix string

    /* CacheDisabled sends no Cache-Control, ETag or Last-Modified, so no conditional request can be answered 304. */
    CacheDisabled bool

    /* CacheMaxAge is the max-age in seconds written while the cache is on: 3600 by default, at least 0, which is "always revalidate", unlimited refused. It is refused beside CacheDisabled. */
    CacheMaxAge bound.Count

    WeakEtag bool

    /* AllowedDotPrefixList names the dot-prefixed first path elements the file server may retrieve; every other dot-prefixed element is refused, and the allowance never reaches past the first element. nil reads as [DefaultAllowedDotPrefix]; an empty list refuses every dot-prefixed path. */
    AllowedDotPrefixList []string

    /* ExcludedPathList names the path prefixes the file server declines without looking at the disk, handing them down the chain to the application. An entry is a prefix of the request path as security.NewPathPrefixMatcher reads it, before the strip prefix and any fold, so a firewall rule and this list select the same requests; a request for the mount root is compared a second time with the index file the root resolves to, so an entry naming the index file declines "/" too; an empty entry switches the server off; an entry without its leading slash gets one. The default excludes nothing. An entry is written decoded, as the router reads the path, with %2F for a separator inside a segment: a percent escape other than %2F excludes only a path whose name holds the percent sign literally. */
    ExcludedPathList []string

    /* Root is the directory PublicDir is joined to in the filesystem mode; "" reads as ".". */
    Root string

    /* FileSystem is the filesystem served in the embedded mode; nil is refused there and it is ignored in the filesystem mode. The content tag of a file with no modification time, every file of an embedded filesystem, is kept per path and size for the life of the server: the bytes of a filesystem without modification times must not change while it serves, or a same-length change is answered 304 over stale bytes; a mutable filesystem must report modification times. */
    FileSystem fs.FS
}

var cacheMaxAgeRule = bound.Rule[int]{
    Name:    "Config.CacheMaxAge",
    Default: 3600,
    Minimum: 0,
}

type fileServerConfig struct {
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

func copyExcludedPathList(excludedPathList []string) []string {
    copied := []string{}
    for _, excludedPath := range excludedPathList {
        /* an entry without a leading slash could never match, so it is normalized; the configuration door refuses one */
        if 0 < len(excludedPath) && '/' != excludedPath[0] {
            excludedPath = "/" + excludedPath
        }

        copied = append(copied, excludedPath)
    }

    return copied
}

func copyAllowedDotPrefixList(allowedDotPrefixList []string) []string {
    if nil == allowedDotPrefixList {
        return []string{DefaultAllowedDotPrefix}
    }

    return append([]string{}, allowedDotPrefixList...)
}

/* resolveFileServerConfig decides every default of the configuration and copies both lists, so the server is immutable once built: requests read these fields with no lock. A max age the rule refuses, or one set beside CacheDisabled, panics naming the field. */
func resolveFileServerConfig(config Config) *fileServerConfig {
    if true == config.CacheDisabled && false == config.CacheMaxAge.IsAbsent() {
        exception.Panic(
            exception.NewError(
                "the cache max age is refused while the cache is disabled",
                exceptioncontract.Context{
                    "field": cacheMaxAgeRule.Name,
                },
                nil,
            ),
        )
    }

    cacheMaxAge, cacheMaxAgeErr := bound.Resolve(config.CacheMaxAge, cacheMaxAgeRule)
    if nil != cacheMaxAgeErr {
        exception.Panic(exception.FromError(cacheMaxAgeErr))
    }

    mode := config.Mode
    if "" == mode {
        mode = ModeFilesystem
    }

    indexFile := config.IndexFile
    if "" == indexFile {
        indexFile = "index.html"
    }

    return &fileServerConfig{
        mode:                 mode,
        publicDir:            config.PublicDir,
        indexFile:            indexFile,
        stripPrefix:          config.StripPrefix,
        enableCache:          false == config.CacheDisabled,
        cacheMaxAge:          cacheMaxAge.Value,
        weakEtag:             config.WeakEtag,
        allowedDotPrefixList: copyAllowedDotPrefixList(config.AllowedDotPrefixList),
        excludedPathList:     copyExcludedPathList(config.ExcludedPathList),
    }
}
