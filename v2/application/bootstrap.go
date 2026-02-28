package application

import (
    "os"
    "path/filepath"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

func ensureRuntimeDirectories(
    projectDirectory string,
    logsDirectory string,
    cacheDirectory string,
) error {
    logsPath := logsDirectory
    if false == filepath.IsAbs(logsPath) {
        logsPath = filepath.Join(projectDirectory, logsDirectory)
    }

    cachePath := cacheDirectory
    if false == filepath.IsAbs(cachePath) {
        cachePath = filepath.Join(projectDirectory, cacheDirectory)
    }

    runtimeDirectories := []string{
        logsPath,
        cachePath,
    }

    for _, directory := range runtimeDirectories {
        if "" == directory {
            continue
        }

        mkdirAllErr := os.MkdirAll(directory, 0o755)
        if nil != mkdirAllErr {
            return exception.NewError(
                "failed to create runtime directory",
                exceptioncontract.Context{
                    "directory": directory,
                },
                mkdirAllErr,
            )
        }
    }

    return nil
}
