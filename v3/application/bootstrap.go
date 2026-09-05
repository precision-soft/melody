package application

import (
    "os"
    "path/filepath"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* resolveRuntimePath anchors a relative runtime path to the project directory, so the one location the configuration names is the one location every working directory reads and writes. An absolute path passes through untouched, and so does the empty one — it means something else to each caller. */
func resolveRuntimePath(projectDirectory string, path string) string {
    if "" == path {
        return path
    }

    if true == filepath.IsAbs(path) {
        return path
    }

    return filepath.Join(projectDirectory, path)
}

func ensureRuntimeDirectories(
    projectDirectory string,
    logsDirectory string,
    cacheDirectory string,
) error {
    logsPath := resolveRuntimePath(projectDirectory, logsDirectory)

    cachePath := resolveRuntimePath(projectDirectory, cacheDirectory)

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
