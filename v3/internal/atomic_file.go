package internal

import (
    "encoding/json"
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

    /* BH-03: an opening brace alone admits malformed text and multiple JSON
       documents, allowing the generators to overwrite a foreign file. Require
       one valid object; absent or empty targets remain accepted above.
       Regressions: TestRefuseNonJsonOutputTarget_RejectsMalformedObject,
       TestRouteManifestCommand_RefusesToOverwriteMalformedJson and
       TestGenerateCommand_RefusesToOverwriteMalformedJson. */
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

    /* BH-02: a valid 255-byte output basename leaves no room for an appended
       random suffix. Use a short independent name in the destination directory
       so rename remains atomic; retain permission preservation and cleanup below.
       Regression: TestWriteFileAtomically_LongValidFilename. */
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
