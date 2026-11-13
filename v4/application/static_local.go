//go:build !melody_static_embedded

package application

import (
    "errors"
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
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
    _ = embeddedPublicFiles

    refuseAnExplicitPublicDirectoryThatIsNotADirectory(configuration)

    staticConfig := static.Config{
        Mode:             static.ModeFilesystem,
        PublicDirectory:  configuration.Http().PublicDirectory(),
        IndexFile:        configuration.Http().StaticIndexFile(),
        CacheDisabled:    false == configuration.Http().StaticEnableCache(),
        ExcludedPathList: configuration.Http().StaticExcludedPaths(),
        Root:             configuration.Kernel().ProjectDirectory(),
    }

    /* the file server refuses a max age beside CacheDisabled */
    if false == staticConfig.CacheDisabled {
        staticConfig.CacheMaxAge = bound.Of(configuration.Http().StaticCacheMaxAge().Value)
    }

    return staticConfig
}

/* the default may be absent, an application with no assets; one the environment names is meant to be served */
func refuseAnExplicitPublicDirectoryThatIsNotADirectory(configuration configcontract.Configuration) {
    /* a configuration that does not carry the key has no explicit value to refuse */
    parameter := configuration.Get(config.KernelPublicDirectory)
    if true == internal.IsNilInterface(parameter) || true == parameter.IsDefault() {
        return
    }

    /* resolved as the file server resolves it */
    publicDirectory := strings.TrimSpace(configuration.Http().PublicDirectory())
    if "" == publicDirectory {
        publicDirectory = "public"
    }

    if false == filepath.IsAbs(publicDirectory) {
        publicDirectory = filepath.Join(configuration.Kernel().ProjectDirectory(), publicDirectory)
    }

    publicDirectoryInfo, publicDirectoryErr := os.Stat(publicDirectory)
    if nil != publicDirectoryErr {
        message := "the public directory cannot be read"
        if true == errors.Is(publicDirectoryErr, fs.ErrNotExist) {
            message = "the public directory does not exist"
        }

        exception.Panic(
            exception.NewError(
                message,
                exceptioncontract.Context{
                    "environmentKey":  config.PublicDirectoryKey,
                    "publicDirectory": publicDirectory,
                },
                publicDirectoryErr,
            ),
        )
    }

    if false == publicDirectoryInfo.IsDir() {
        exception.Panic(
            exception.NewError(
                "the public directory is a file",
                exceptioncontract.Context{
                    "environmentKey":  config.PublicDirectoryKey,
                    "publicDirectory": publicDirectory,
                },
                nil,
            ),
        )
    }
}
