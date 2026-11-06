package migrate

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

/* migrationFileMode is the permission the rewrite falls back to when the destination cannot be read; the mode the destination carries, bun's request narrowed by the process umask, is what the rewrite keeps. */
const migrationFileMode = os.FileMode(0o644)

/* errDirectorySyncAfterRename marks the one failure of finishFileAtomically that leaves the destination whole: only the directory fsync failed, so a caller reports it as a warning, since a re-run would create a second migration beside the first. */
var errDirectorySyncAfterRename = errors.New("the directory entry could not be fsynced after the rename")

/* finishFileAtomically replaces a file with the same content through a temporary neighbour, fsynced, renamed over the destination, and the directory fsynced so the rename is durable. bun writes a Go migration with a single os.WriteFile, which a crash can leave truncated, so this finishes the job over the file it produced. It is duplicated here because the framework's atomic writer is internal to another module. */
func finishFileAtomically(destination string, content []byte) error {
    directory := filepath.Dir(destination)

    /* the temp name does not carry the destination's basename, which may fill the 255 bytes of a path component on its own */
    tmpFile, tmpErr := os.CreateTemp(directory, ".melody-migrate-*.tmp")
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

    /* CreateTemp makes the file 0600, and the destination's own mode is what the rewrite keeps; the constant is the fallback for a destination that cannot be read */
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

/* syncDirectoryAfterRename is the directory fsync after the rename, held in a variable so a test can make exactly that step fail. */
var syncDirectoryAfterRename = syncDirectory

/* syncDirectory makes the rename itself durable: without it the file's content survives a crash and the directory entry naming it need not. */
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
