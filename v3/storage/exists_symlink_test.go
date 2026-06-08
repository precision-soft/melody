package storage_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/precision-soft/melody/v3/storage"
)

func TestLocalStorage_ExistsDoesNotFollowSymlinkToExternalTarget(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    secret := filepath.Join(outside, "secret.txt")
    if writeErr := os.WriteFile(secret, []byte("top secret"), 0o600); nil != writeErr {
        t.Fatalf("seed external file: %v", writeErr)
    }

    if linkErr := os.Symlink(secret, filepath.Join(base, "leak")); nil != linkErr {
        t.Fatalf("symlink: %v", linkErr)
    }

    local := storage.NewLocalStorage(base)

    exists, _ := local.Exists(testRuntime(), "leak")
    if true == exists {
        t.Fatalf("Exists followed a symlink and reported an external target as existing")
    }
}
