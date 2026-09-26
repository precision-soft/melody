package internal

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

/* RefuseNonJsonOutputTarget refuses to replace an existing file that is not a JSON document, since a generated json artifact replaces its target whole and such a file is someone's source. */
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

    /* the opening brace alone does not make a JSON document: "{ notes", an object followed by prose and two concatenated objects are someone's source too */
    if true == strings.HasPrefix(trimmedContent, "{") && true == json.Valid([]byte(trimmedContent)) {
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

/* WriteFileAtomically writes through a temp file and a rename, so a write that dies partway leaves the previous artifact intact. The parent directories are created. */
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

    /* the temp name does not carry the output's basename, which may fill the 255 bytes of a path component on its own */
    tempFile, tempErr := os.CreateTemp(directoryPath, ".melody-artifact-*.tmp")
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

    /* the bytes are flushed before the rename, so a crash cannot publish an empty artifact */
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

    /* the temp file is born 0600: the destination's mode is kept, 0644 for a new file */
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

    /* the directory is synced so the rename itself survives a crash; the artifact is already in place, so the failure is only reported */
    if directorySyncErr := syncDirectory(directoryPath); nil != directorySyncErr {
        return exception.NewError(
            "could not fsync the output directory of the "+artifactName,
            map[string]any{"out": outputPath},
            directorySyncErr,
        )
    }

    return nil
}

func destinationFileMode(outputPath string) os.FileMode {
    info, statErr := os.Stat(outputPath)
    if nil != statErr {
        return 0o644
    }

    return info.Mode().Perm()
}

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
