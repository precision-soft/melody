//go:build !melody_env_embedded

package application

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestMissingEnvironmentFileHint_NamesTheRemedyWhenNoEnvFile(t *testing.T) {
    directory := t.TempDir()

    hint := missingEnvironmentFileHint(directory)
    if "" == hint {
        t.Fatalf("expected an actionable hint when the directory has no .env")
    }

    if false == strings.Contains(hint, "no .env or .env.local file was found in "+directory) {
        t.Fatalf("expected the hint to name the directory itself, got %q", hint)
    }

    if true == strings.Contains(hint, "next to the executable") || true == strings.Contains(hint, "beside the binary") {
        t.Fatalf("expected the hint not to claim the directory holds the executable (go run resolves the working directory), got %q", hint)
    }

    if false == strings.Contains(hint, "melody_env_embedded") {
        t.Fatalf("expected the hint to mention the embedded build tag, got %q", hint)
    }
}

func TestMissingEnvironmentFileHint_EmptyWhenEnvPresent(t *testing.T) {
    directory := t.TempDir()

    if writeErr := os.WriteFile(filepath.Join(directory, ".env"), []byte("APP_NAME=demo\n"), 0o600); nil != writeErr {
        t.Fatalf("write .env: %v", writeErr)
    }

    if "" != missingEnvironmentFileHint(directory) {
        t.Fatalf("expected no hint when a .env is present")
    }
}

func TestMissingEnvironmentFileHint_EmptyWhenNoDirectory(t *testing.T) {
    if "" != missingEnvironmentFileHint("") {
        t.Fatalf("expected no hint for an empty directory")
    }
}
