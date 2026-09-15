package migrate

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

const migrationFileMode = os.FileMode(0o644)

var errDirectorySyncAfterRename = errors.New("the directory entry could not be fsynced after the rename")

func finishFileAtomically(destination string, content []byte) error {
    directory := filepath.Dir(destination)

    tmpFile, tmpErr := os.CreateTemp(directory, ".migration-*")
    if nil != tmpErr {
        return fmt.Errorf("could not create the temporary file beside %s: %w", destination, tmpErr)
    }

    tmpPath := tmpFile.Name()
    renamed := false
    defer func() {
        if false == renamed {
            _ = os.Remove(tmpPath)
        }
    }()

    if _, writeErr := tmpFile.Write(content); nil != writeErr {
        _ = tmpFile.Close()

        return fmt.Errorf("could not write the temporary file %s: %w", tmpPath, writeErr)
    }

    if syncErr := tmpFile.Sync(); nil != syncErr {
        _ = tmpFile.Close()

        return fmt.Errorf("could not fsync the temporary file %s: %w", tmpPath, syncErr)
    }

    if closeErr := tmpFile.Close(); nil != closeErr {
        return fmt.Errorf("could not close the temporary file %s: %w", tmpPath, closeErr)
    }

    if chmodErr := os.Chmod(tmpPath, destinationFileMode(destination)); nil != chmodErr {
        return fmt.Errorf("could not chmod the temporary file %s: %w", tmpPath, chmodErr)
    }

    if renameErr := os.Rename(tmpPath, destination); nil != renameErr {
        return fmt.Errorf("could not rename %s over %s: %w", tmpPath, destination, renameErr)
    }

    renamed = true

    if syncErr := syncDirectoryAfterRename(directory); nil != syncErr {
        return fmt.Errorf("%w: %w", errDirectorySyncAfterRename, syncErr)
    }

    return nil
}

func destinationFileMode(destination string) os.FileMode {
    info, statErr := os.Stat(destination)
    if nil != statErr {
        return migrationFileMode
    }

    return info.Mode().Perm()
}

var syncDirectoryAfterRename = syncDirectory

func syncDirectory(path string) error {
    directory, openErr := os.Open(path)
    if nil != openErr {
        return fmt.Errorf("could not open the directory %s for fsync: %w", path, openErr)
    }

    syncErr := directory.Sync()
    closeErr := directory.Close()

    if nil != syncErr || nil != closeErr {
        return fmt.Errorf("could not fsync the directory %s: %w", path, errors.Join(syncErr, closeErr))
    }

    return nil
}
