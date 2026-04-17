package static

import (
    "errors"
    "io/fs"
    "os"
    "path/filepath"
    "runtime"
    "testing"
)

func TestDirFileSystem_OpenRootReturnsDirectory(t *testing.T) {
    dir := t.TempDir()

    fileSystem := osDirFileSystem(dir)

    file, err := fileSystem.Open("")
    if nil != err {
        t.Fatalf("open root error: %v", err)
    }
    defer file.Close()

    info, err := file.Stat()
    if nil != err {
        t.Fatalf("stat error: %v", err)
    }

    if false == info.IsDir() {
        t.Fatalf("expected directory")
    }
}

func TestDirFileSystem_OpenPathWithinRootSucceeds(t *testing.T) {
    dir := t.TempDir()

    filePath := filepath.Join(dir, "file.txt")
    writeErr := os.WriteFile(filePath, []byte("hello"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    fileSystem := osDirFileSystem(dir)

    file, err := fileSystem.Open("file.txt")
    if nil != err {
        t.Fatalf("open error: %v", err)
    }
    defer file.Close()
}

func TestDirFileSystem_OpenAbsolutePathRejected(t *testing.T) {
    dir := t.TempDir()

    fileSystem := osDirFileSystem(dir)

    _, err := fileSystem.Open("/etc/passwd")
    if false == errors.Is(err, fs.ErrInvalid) {
        t.Fatalf("expected fs.ErrInvalid, got %v", err)
    }
}

func TestDirFileSystem_OpenParentTraversalRejected(t *testing.T) {
    dir := t.TempDir()

    fileSystem := osDirFileSystem(dir)

    _, err := fileSystem.Open("..")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }

    _, err = fileSystem.Open("../secret.txt")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }
}

func TestDirFileSystem_OpenSymlinkEscapingRootRejected(t *testing.T) {
    if "windows" == runtime.GOOS {
        t.Skip("symlink behavior differs on windows")
    }

    outsideDir := t.TempDir()
    outsideFile := filepath.Join(outsideDir, "secret.txt")
    writeErr := os.WriteFile(outsideFile, []byte("secret"), 0o644)
    if nil != writeErr {
        t.Fatalf("write outside error: %v", writeErr)
    }

    rootDir := t.TempDir()
    linkPath := filepath.Join(rootDir, "link.txt")
    if linkErr := os.Symlink(outsideFile, linkPath); nil != linkErr {
        t.Fatalf("symlink error: %v", linkErr)
    }

    fileSystem := osDirFileSystem(rootDir)

    _, err := fileSystem.Open("link.txt")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }
}

func TestDirFileSystem_OpenSymlinkWithinRootAllowed(t *testing.T) {
    if "windows" == runtime.GOOS {
        t.Skip("symlink behavior differs on windows")
    }

    rootDir := t.TempDir()

    targetPath := filepath.Join(rootDir, "target.txt")
    if writeErr := os.WriteFile(targetPath, []byte("data"), 0o644); nil != writeErr {
        t.Fatalf("write target error: %v", writeErr)
    }

    linkPath := filepath.Join(rootDir, "link.txt")
    if linkErr := os.Symlink(targetPath, linkPath); nil != linkErr {
        t.Fatalf("symlink error: %v", linkErr)
    }

    fileSystem := osDirFileSystem(rootDir)

    file, err := fileSystem.Open("link.txt")
    if nil != err {
        t.Fatalf("open error: %v", err)
    }
    defer file.Close()
}

func TestDirFileSystem_OpenNonExistentPathReturnsError(t *testing.T) {
    dir := t.TempDir()

    fileSystem := osDirFileSystem(dir)

    _, err := fileSystem.Open("does-not-exist.txt")
    if nil == err {
        t.Fatalf("expected error")
    }
}
