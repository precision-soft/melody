package application

import (
    "os"
    "path/filepath"
    "testing"
)

func TestEnsureRuntimeDirectories_CreatesDirectories(t *testing.T) {
    projectDirectory := t.TempDir()

    relativeLogsDirectory := filepath.Join("var", "log")
    relativeCacheDirectory := filepath.Join("var", "cache")

    err := ensureRuntimeDirectories(projectDirectory, relativeLogsDirectory, relativeCacheDirectory)
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

    err := ensureRuntimeDirectories(projectDirectory, "", "")
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

    err = ensureRuntimeDirectories(projectDirectory, filepath.Join("var", "log"), "")
    if nil == err {
        t.Fatalf("expected error")
    }
}
