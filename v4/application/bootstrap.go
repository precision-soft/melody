package application

import (
    "os"
    "path/filepath"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
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

const cacheDirectoryMode os.FileMode = 0o755

func ensureRuntimeDirectories(
    projectDirectory string,
    logsDirectory string,
    cacheDirectory string,
    logDirectoryMode os.FileMode,
) error {
    logsPath := resolveRuntimePath(projectDirectory, logsDirectory)

    cachePath := resolveRuntimePath(projectDirectory, cacheDirectory)

    /* the cache directory has no key of its own yet, and the journal's must not govern it */
    runtimeDirectories := []struct {
        path string
        mode os.FileMode
    }{
        {path: logsPath, mode: logDirectoryMode},
        {path: cachePath, mode: cacheDirectoryMode},
    }

    for _, runtimeDirectory := range runtimeDirectories {
        directory := runtimeDirectory.path
        if "" == directory {
            continue
        }

        mkdirAllErr := os.MkdirAll(directory, runtimeDirectory.mode)
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
