package migrate

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

/* migrationFileMode is the permission the finished migration file carries. It is bun's own 0644 for the file it wrote, kept so the rewrite below replaces the content and nothing else. */
const migrationFileMode = os.FileMode(0o644)

/*
finishFileAtomically replaces a file with the same content, written the way a
file that must survive a crash has to be written: into a temporary neighbour,
fsynced, renamed over the destination, and the destination's DIRECTORY fsynced so
the rename itself is durable.

It exists because bun creates a Go migration with a single os.WriteFile straight
onto the destination path, which is not atomic: a crash, a full disk or a killed
process in the middle of that write leaves a TRUNCATED Go file in the migrations
directory — one that does not compile, under a timestamped name whose position in
the sequence looks entirely legitimate. Taking the write away from bun is not
possible, because the directory it writes into is unexported; what is possible is
to finish the job over the file it produced, so that a command reporting success
has left a whole and durable file behind. A crash before that point leaves bun's
partial file, but the command did not report success either, which is the same
answer a failed create has always given.

The helper is duplicated here rather than imported: the framework's own atomic
writer lives in an internal package, which a separate module cannot reach, and
the cron generator carries the same shape beside its own manifests for the same
reason.
*/
func finishFileAtomically(destination string, content []byte) error {
    directory := filepath.Dir(destination)

    tmpFile, tmpErr := os.CreateTemp(directory, "."+filepath.Base(destination)+".*")
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

    /* CreateTemp makes the file 0600; the destination carries the mode bun gave it, so the rewrite is invisible to anything reading the directory */
    if chmodErr := os.Chmod(tmpPath, migrationFileMode); nil != chmodErr {
        return fmt.Errorf("could not chmod the temporary file %s: %w", tmpPath, chmodErr)
    }

    if renameErr := os.Rename(tmpPath, destination); nil != renameErr {
        return fmt.Errorf("could not rename %s over %s: %w", tmpPath, destination, renameErr)
    }

    renamed = true

    return syncDirectory(directory)
}

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
