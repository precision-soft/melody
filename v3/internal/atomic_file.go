package internal

import (
    "os"
    "path/filepath"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

/* RefuseNonJsonOutputTarget refuses to replace a file that is not a JSON document. Every command that writes a generated json artifact replaces its target whole, so an existing file holding anything else is someone's source a mistyped --out points at rather than a previous output of this command, and overwriting it is not recoverable. */
func RefuseNonJsonOutputTarget(outputPath string, artifactName string) *exception.Error {
    existingContent, readErr := os.ReadFile(outputPath)
    if nil != readErr && false == os.IsNotExist(readErr) {
        return exception.NewError(
            "could not inspect the existing output file",
            map[string]any{"out": outputPath},
            readErr,
        )
    }

    if nil != readErr {
        return nil
    }

    trimmedContent := strings.TrimSpace(string(existingContent))
    if "" == trimmedContent {
        return nil
    }

    if true == strings.HasPrefix(trimmedContent, "{") {
        return nil
    }

    return exception.NewError(
        "the output file exists and is not a JSON document; remove it or choose another path",
        map[string]any{
            "out":      outputPath,
            "artifact": artifactName,
        },
        nil,
    )
}

/* WriteFileAtomically lands a generated artifact through a temp file and a rename, so a write that dies partway — a full disk, a killed process — leaves the previous artifact intact instead of a torn file published as the thing it describes. The parent directories are created, because a fresh checkout has none of them and the raw open error names nothing. */
func WriteFileAtomically(outputPath string, payload []byte, artifactName string) *exception.Error {
    directoryPath := filepath.Dir(outputPath)

    makeDirectoryErr := os.MkdirAll(directoryPath, 0o755)
    if nil != makeDirectoryErr {
        return exception.NewError(
            "could not create the output directory of the "+artifactName,
            map[string]any{"out": outputPath},
            makeDirectoryErr,
        )
    }

    tempFile, tempErr := os.CreateTemp(directoryPath, filepath.Base(outputPath)+".*.tmp")
    if nil != tempErr {
        return exception.NewError(
            "could not create the temp file of the "+artifactName,
            map[string]any{"out": outputPath},
            tempErr,
        )
    }

    tempPath := tempFile.Name()

    _, writeErr := tempFile.Write(payload)
    if nil != writeErr {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not write the "+artifactName,
            map[string]any{"out": outputPath},
            writeErr,
        )
    }

    /* flush the bytes before the rename so a crash after the rename cannot publish a present-but-empty artifact under the name it describes; the sibling writers in migrate and cron sync for the same reason */
    if syncErr := tempFile.Sync(); nil != syncErr {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not flush the "+artifactName,
            map[string]any{"out": outputPath},
            syncErr,
        )
    }

    closeErr := tempFile.Close()
    if nil != closeErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not close the temp file of the "+artifactName,
            map[string]any{"out": outputPath},
            closeErr,
        )
    }

    /* the temp file is born 0600; keep the mode the destination already carries so a deliberately-0600 file is not silently widened, and fall back to 0644 — the mode a direct write gives a new file — when there is no destination to read. The old unconditional chmod 0644 reset a 0600 file to world-readable on every rewrite. Mirrors the migrate writer's destinationFileMode. */
    chmodErr := os.Chmod(tempPath, destinationFileMode(outputPath))
    if nil != chmodErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not set the mode of the "+artifactName,
            map[string]any{"out": outputPath},
            chmodErr,
        )
    }

    renameErr := os.Rename(tempPath, outputPath)
    if nil != renameErr {
        _ = os.Remove(tempPath)

        return exception.NewError(
            "could not replace the output file with the "+artifactName,
            map[string]any{"out": outputPath},
            renameErr,
        )
    }

    /* fsync the directory that received the rename, where the file's NAME lives — the temp file's own Sync covered only its bytes, so without this the content survives a crash and the entry naming it need not. The artifact is already in place; a caller of these commands regenerates idempotently, so reporting the failure is safe. */
    if directorySyncErr := syncDirectory(directoryPath); nil != directorySyncErr {
        return exception.NewError(
            "could not fsync the output directory of the "+artifactName,
            map[string]any{"out": outputPath},
            directorySyncErr,
        )
    }

    return nil
}

/* destinationFileMode reads the permission the destination already carries so an atomic rewrite keeps it, and falls back to 0644 — the mode a direct write gives a new file — when the destination cannot be read. */
func destinationFileMode(outputPath string) os.FileMode {
    info, statErr := os.Stat(outputPath)
    if nil != statErr {
        return 0o644
    }

    return info.Mode().Perm()
}

/* syncDirectory fsyncs the directory that received the rename so the rename itself is durable: without it the file's content survives a crash and the directory entry naming it need not. */
func syncDirectory(directoryPath string) error {
    directory, openErr := os.Open(directoryPath)
    if nil != openErr {
        return openErr
    }

    syncErr := directory.Sync()
    closeErr := directory.Close()

    if nil != syncErr {
        return syncErr
    }

    return closeErr
}
