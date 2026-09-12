package migrate

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

/* migrationFileMode is used when the destination mode cannot be read. An existing destination keeps its permissions, including restrictions applied by the process umask. */
const migrationFileMode = os.FileMode(0o644)

/* errDirectorySyncAfterRename indicates that the complete file is in place, but durability of its directory entry could not be confirmed. */
var errDirectorySyncAfterRename = errors.New("the directory entry could not be fsynced after the rename")

/* finishFileAtomically rewrites Bun's generated file through a synced temporary neighbour, preserves its permissions, renames it over the destination, and syncs the directory. Failure before rename leaves the original destination in place and removes the temporary file. */
func finishFileAtomically(destination string, content []byte) error {
    directory := filepath.Dir(destination)

    /* Keep the temporary component short even when the destination uses the filesystem's full name limit. */
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

    /* Preserve the destination permissions instead of publishing CreateTemp's 0600 mode. */
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

/* destinationFileMode reads the permission the destination carries, and falls back to bun's own 0644 when it cannot be read. */
func destinationFileMode(destination string) os.FileMode {
    info, statErr := os.Stat(destination)
    if nil != statErr {
        return migrationFileMode
    }

    return info.Mode().Perm()
}

/* syncDirectoryAfterRename allows tests to exercise a directory-sync failure after a successful rename. */
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
