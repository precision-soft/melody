//go:build !melody_static_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/http/static"
)

func newStaticFileServerConfig(
    embeddedPublicFiles fs.FS,
    configuration configcontract.Configuration,
) static.Config {
    _ = embeddedPublicFiles

    staticConfig := static.Config{
        Mode:             static.ModeFilesystem,
        PublicDir:        configuration.Http().PublicDir(),
        IndexFile:        configuration.Http().StaticIndexFile(),
        CacheDisabled:    false == configuration.Http().StaticEnableCache(),
        ExcludedPathList: configuration.Http().StaticExcludedPaths(),
        Root:             configuration.Kernel().ProjectDir(),
    }

    /* the file server refuses a max age beside CacheDisabled */
    if false == staticConfig.CacheDisabled {
        staticConfig.CacheMaxAge = bound.Of(configuration.Http().StaticCacheMaxAge().Value)
    }

    return staticConfig
}
