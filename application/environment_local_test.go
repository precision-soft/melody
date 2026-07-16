//go:build !melody_env_embedded

package application

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/config"
)

func TestMissingEnvironmentFileHint_NamesTheRemedyWhenNoEnvFile(t *testing.T) {
    directory := t.TempDir()

    hint := missingEnvironmentFileHint(directory)
    if "" == hint {
        t.Fatalf("expected an actionable hint when the directory has no .env")
    }

    if false == strings.Contains(hint, "no .env, .env.local or .env."+config.EnvDevelopment+" file was found in "+directory) {
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

/** @info a project configured solely through the development-environment file boots and loads it without any .env, so a resolution failure there is a plain unresolved key — a hint claiming the environment files are missing would misattribute it. */
func TestMissingEnvironmentFileHint_EmptyWhenDevelopmentEnvPresent(t *testing.T) {
    cases := []string{
        ".env." + config.EnvDevelopment,
        ".env." + config.EnvDevelopment + ".local",
    }

    for _, fileName := range cases {
        t.Run(fileName, func(t *testing.T) {
            directory := t.TempDir()

            if writeErr := os.WriteFile(filepath.Join(directory, fileName), []byte("APP_NAME=demo\n"), 0o600); nil != writeErr {
                t.Fatalf("write %s: %v", fileName, writeErr)
            }

            if "" != missingEnvironmentFileHint(directory) {
                t.Fatalf("expected no hint when %s is present", fileName)
            }
        })
    }
}
