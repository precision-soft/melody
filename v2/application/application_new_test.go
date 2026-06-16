package application

import (
    "os"
    "path/filepath"
    "testing"
)

func TestFindProjectRootStartingFrom_FindsGoMod(t *testing.T) {
    projectDirectory := t.TempDir()

    err := os.WriteFile(filepath.Join(projectDirectory, "go.mod"), []byte("module example.com/test\n"), 0o644)
    if nil != err {
        t.Fatalf("failed to create go.mod: %v", err)
    }

    subDirectory := filepath.Join(projectDirectory, "a", "b")
    err = os.MkdirAll(subDirectory, 0o755)
    if nil != err {
        t.Fatalf("failed to create sub directory: %v", err)
    }

    resolvedProjectDirectory, err := findProjectRootStartingFrom(subDirectory)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if projectDirectory != resolvedProjectDirectory {
        t.Fatalf("expected %q, got %q", projectDirectory, resolvedProjectDirectory)
    }
}

func TestFindProjectRootStartingFrom_ReturnsErrorWhenNotFound(t *testing.T) {
    directory := t.TempDir()

    resolvedProjectDirectory, err := findProjectRootStartingFrom(directory)
    if nil == err {
        t.Fatalf("expected error")
    }
    if "" != resolvedProjectDirectory {
        t.Fatalf("expected empty directory, got %q", resolvedProjectDirectory)
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnv(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnvLocal(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env.local"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env.local: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env.local is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_ReturnsFalseWhenAbsent(t *testing.T) {
    directory := t.TempDir()

    if true == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected false when no .env files are present")
    }
}
