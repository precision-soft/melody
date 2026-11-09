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

    refuseAnExplicitPublicDirThatIsNotADirectory(configuration)

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

/* the default may be absent, an application with no assets; one the environment names is meant to be served */
func refuseAnExplicitPublicDirThatIsNotADirectory(configuration configcontract.Configuration) {
    /* a configuration that does not carry the key has no explicit value to refuse */
    parameter := configuration.Get(config.KernelPublicDir)
    if true == internal.IsNilInterface(parameter) || true == parameter.IsDefault() {
        return
    }

    /* resolved as the file server resolves it */
    publicDir := strings.TrimSpace(configuration.Http().PublicDir())
    if "" == publicDir {
        publicDir = "public"
    }

    if false == filepath.IsAbs(publicDir) {
        publicDir = filepath.Join(configuration.Kernel().ProjectDir(), publicDir)
    }

    publicDirInfo, publicDirErr := os.Stat(publicDir)
    if nil != publicDirErr {
        message := "the public directory cannot be read"
        if true == errors.Is(publicDirErr, fs.ErrNotExist) {
            message = "the public directory does not exist"
        }

        exception.Panic(
            exception.NewError(
                message,
                exceptioncontract.Context{
                    "environmentKey": config.PublicDirKey,
                    "publicDir":      publicDir,
                },
                publicDirErr,
            ),
        )
    }

    if false == publicDirInfo.IsDir() {
        exception.Panic(
            exception.NewError(
                "the public directory is a file",
                exceptioncontract.Context{
                    "environmentKey": config.PublicDirKey,
                    "publicDir":      publicDir,
                },
                nil,
            ),
        )
    }
}
