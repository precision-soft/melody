package application

import (
    "os"
    "path/filepath"
    "syscall"
    "testing"
)

func TestEnsureRuntimeDirectories_CreatesDirectories(t *testing.T) {
    projectDirectory := t.TempDir()

    relativeLogsDirectory := filepath.Join("var", "log")
    relativeCacheDirectory := filepath.Join("var", "cache")

    err := ensureRuntimeDirectories(projectDirectory, relativeLogsDirectory, relativeCacheDirectory, 0o750)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    logsPath := filepath.Join(projectDirectory, relativeLogsDirectory)
    cachePath := filepath.Join(projectDirectory, relativeCacheDirectory)

    logsInfo, err := os.Stat(logsPath)
    if nil != err {
        t.Fatalf("expected logs dir to exist: %v", err)
    }
    if false == logsInfo.IsDir() {
        t.Fatalf("expected logs path to be a directory")
    }

    cacheInfo, err := os.Stat(cachePath)
    if nil != err {
        t.Fatalf("expected cache dir to exist: %v", err)
    }
    if false == cacheInfo.IsDir() {
        t.Fatalf("expected cache path to be a directory")
    }
}

func TestEnsureRuntimeDirectories_IgnoresEmpty(t *testing.T) {
    projectDirectory := t.TempDir()

    err := ensureRuntimeDirectories(projectDirectory, "", "", 0o750)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestEnsureRuntimeDirectories_ReturnsErrorWhenPathIsFile(t *testing.T) {
    projectDirectory := t.TempDir()

    logsPath := filepath.Join(projectDirectory, "var", "log")
    err := os.MkdirAll(filepath.Dir(logsPath), 0o755)
    if nil != err {
        t.Fatalf("failed to create parent directory: %v", err)
    }

    err = os.WriteFile(logsPath, []byte("file"), 0o644)
    if nil != err {
        t.Fatalf("failed to create file: %v", err)
    }

    err = ensureRuntimeDirectories(projectDirectory, filepath.Join("var", "log"), "", 0o750)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestResolveRuntimePath_AnchorsOnlyARelativePath(t *testing.T) {
    if filepath.Join("/project", "custom", "app.log") != resolveRuntimePath("/project", filepath.Join("custom", "app.log")) {
        t.Fatalf("expected the relative path anchored to the project directory")
    }

    if filepath.Join("/absolute", "app.log") != resolveRuntimePath("/project", filepath.Join("/absolute", "app.log")) {
        t.Fatalf("expected the absolute path untouched")
    }

    if "" != resolveRuntimePath("/project", "") {
        t.Fatalf("expected the empty path untouched")
    }
}

func TestEnsureRuntimeDirectories_TheLogsDirectoryTakesTheLogModeAndTheCacheDirectoryKeeps0755(t *testing.T) {
    previousUmask := syscall.Umask(0)
    defer syscall.Umask(previousUmask)

    projectDirectory := t.TempDir()

    err := ensureRuntimeDirectories(projectDirectory, filepath.Join("var", "log"), filepath.Join("cache"), 0o710)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    for path, expected := range map[string]os.FileMode{filepath.Join(projectDirectory, "var", "log"): 0o710, filepath.Join(projectDirectory, "var"): 0o710, filepath.Join(projectDirectory, "cache"): 0o755} {
        information, statErr := os.Stat(path)
        if nil != statErr {
            t.Fatalf("unexpected stat error on %s: %v", path, statErr)
        }

        if expected != information.Mode().Perm() {
            t.Fatalf("expected %s at mode %o, got %o", path, expected, information.Mode().Perm())
        }
    }
}
