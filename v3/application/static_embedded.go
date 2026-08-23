//go:build melody_static_embedded

package application

import (
    "io/fs"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http/static"
    "github.com/precision-soft/melody/v3/internal"
)

func newStaticFileServerOptions(
    embeddedPublicFiles fs.FS,
    configuration configcontract.Configuration,
) *static.Options {
    /* the guard reads through the interface the way its environment sibling does: a typed-nil fs.FS passes the plain comparison and dies later as an anonymous nil dereference inside fs.Stat, instead of this refusal that names the argument */
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

    fileServerConfig := static.NewFileServerConfig(
        static.ModeEmbedded,
        configuration.Http().PublicDir(),
        configuration.Http().StaticIndexFile(),
        "",
        configuration.Http().StaticEnableCache(),
        configuration.Http().StaticCacheMaxAge(),
        false,
    )

    fileServerConfig.SetExcludedPathList(configuration.Http().StaticExcludedPaths())

    return static.NewOptions(
        fileServerConfig,
        "",
        embeddedPublicFiles,
    )
}
