package internal

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestRefuseNonJsonOutputTarget_RefusesSomeonesSource(t *testing.T) {
    directory := t.TempDir()
    targetPath := filepath.Join(directory, "main.go")

    if writeErr := os.WriteFile(targetPath, []byte("package main\n"), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    refusalErr := RefuseNonJsonOutputTarget(targetPath, "route manifest")
    if nil == refusalErr {
        t.Fatalf("expected a non-JSON target to be refused")
    }

    if false == strings.Contains(refusalErr.Error(), "not a JSON document") {
        t.Fatalf("unexpected refusal: %v", refusalErr)
    }
}

func TestRefuseNonJsonOutputTarget_AcceptsAnAbsentOrEmptyOrJsonTarget(t *testing.T) {
    directory := t.TempDir()

    if refusalErr := RefuseNonJsonOutputTarget(filepath.Join(directory, "absent.json"), "route manifest"); nil != refusalErr {
        t.Fatalf("an absent target must be accepted, got %v", refusalErr)
    }

    emptyPath := filepath.Join(directory, "empty.json")
    if writeErr := os.WriteFile(emptyPath, []byte("   "), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    if refusalErr := RefuseNonJsonOutputTarget(emptyPath, "route manifest"); nil != refusalErr {
        t.Fatalf("an empty target must be accepted, got %v", refusalErr)
    }

    jsonPath := filepath.Join(directory, "previous.json")
    if writeErr := os.WriteFile(jsonPath, []byte(`{"routes":[]}`), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    if refusalErr := RefuseNonJsonOutputTarget(jsonPath, "route manifest"); nil != refusalErr {
        t.Fatalf("a previous output must be accepted, got %v", refusalErr)
    }
}

func TestWriteFileAtomically_CreatesTheParentDirectoriesAndLeavesNoTempResidue(t *testing.T) {
    directory := t.TempDir()
    targetPath := filepath.Join(directory, "public", "nested", "routes.json")

    if writeErr := WriteFileAtomically(targetPath, []byte(`{"routes":[]}`), "route manifest"); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    written, readErr := os.ReadFile(targetPath)
    if nil != readErr {
        t.Fatalf("read back: %v", readErr)
    }

    if `{"routes":[]}` != string(written) {
        t.Fatalf("unexpected content: %q", string(written))
    }

    info, statErr := os.Stat(targetPath)
    if nil != statErr {
        t.Fatalf("stat: %v", statErr)
    }

    /* the temp file is born 0600; the artifact keeps the mode a direct write would have given it */
    if 0o644 != info.Mode().Perm() {
        t.Fatalf("unexpected mode: %v", info.Mode().Perm())
    }

    entries, readDirErr := os.ReadDir(filepath.Dir(targetPath))
    if nil != readDirErr {
        t.Fatalf("read dir: %v", readDirErr)
    }

    if 1 != len(entries) {
        t.Fatalf("expected only the artifact to remain, got %d entries", len(entries))
    }
}

func TestWriteFileAtomically_ReplacesAPreviousArtifact(t *testing.T) {
    directory := t.TempDir()
    targetPath := filepath.Join(directory, "routes.json")

    if writeErr := os.WriteFile(targetPath, []byte(`{"routes":[{"name":"old"}]}`), 0o644); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    if writeErr := WriteFileAtomically(targetPath, []byte(`{"routes":[{"name":"new"}]}`), "route manifest"); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    written, readErr := os.ReadFile(targetPath)
    if nil != readErr {
        t.Fatalf("read back: %v", readErr)
    }

    if false == strings.Contains(string(written), "new") {
        t.Fatalf("unexpected content: %q", string(written))
    }
}

func TestWriteFileAtomically_KeepsADeliberate0600DestinationMode(t *testing.T) {
    directory := t.TempDir()
    targetPath := filepath.Join(directory, "secret.json")

    if writeErr := os.WriteFile(targetPath, []byte(`{"old":true}`), 0o600); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }
    if chmodErr := os.Chmod(targetPath, 0o600); nil != chmodErr {
        t.Fatalf("chmod seed: %v", chmodErr)
    }

    if writeErr := WriteFileAtomically(targetPath, []byte(`{"new":true}`), "secret document"); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    info, statErr := os.Stat(targetPath)
    if nil != statErr {
        t.Fatalf("stat: %v", statErr)
    }

    if 0o600 != info.Mode().Perm() {
        t.Fatalf("expected the deliberate 0600 destination mode to be kept, not widened, got %v", info.Mode().Perm())
    }
}
