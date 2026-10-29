//go:build melody_static_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/internal"
)

func newStaticFileServerConfig(
    embeddedPublicFiles fs.FS,
    configuration configcontract.Configuration,
) static.Config {
    /* read through the interface: a typed-nil fs.FS passes the plain comparison and would die later inside fs.Stat instead of in this refusal that names the argument */
    if true == internal.IsNilInterface(embeddedPublicFiles) {
        exception.Panic(
            exception.NewError(
                "embedded public files are not provided",
                exceptioncontract.Context{
                    "buildTag":        "melody_static_embedded",
                    "publicDirectory": configuration.Http().PublicDir(),
                },
                nil,
            ),
        )
    }

    staticConfig := static.Config{
        Mode:             static.ModeEmbedded,
        PublicDir:        configuration.Http().PublicDir(),
        IndexFile:        configuration.Http().StaticIndexFile(),
        CacheDisabled:    false == configuration.Http().StaticEnableCache(),
        ExcludedPathList: configuration.Http().StaticExcludedPaths(),
        Root:             "",
        FileSystem:       embeddedPublicFiles,
    }

    /* the file server refuses a max age beside CacheDisabled */
    if false == staticConfig.CacheDisabled {
        staticConfig.CacheMaxAge = bound.Of(configuration.Http().StaticCacheMaxAge().Value)
    }

    return staticConfig
}
