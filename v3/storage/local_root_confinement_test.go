package storage_test

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/storage"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func TestLocalStorage_RejectsIntermediateDirectorySymlinkEscapeOnPut(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    /** @important an escaping symlink at an intermediate path component, not the leaf — O_NOFOLLOW only guards the leaf, so this is the path os.Root closes */
    if linkErr := os.Symlink(outside, filepath.Join(base, "evil")); nil != linkErr {
        t.Fatalf("create intermediate symlink: %v", linkErr)
    }

    local := storage.NewLocalStorage(base)
    content := "should not escape"

    putErr := local.Put(testRuntime(), "evil/object.txt", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatalf("expected an escape through an intermediate-directory symlink to be rejected")
    }

    if _, statErr := os.Stat(filepath.Join(outside, "object.txt")); false == os.IsNotExist(statErr) {
        t.Fatalf("an object escaped the base directory through an intermediate symlink, stat err: %v", statErr)
    }
}

func TestLocalStorage_RejectsAbsoluteKeyEscape(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())

    if _, getErr := local.Get(testRuntime(), "/etc/passwd"); nil == getErr {
        t.Fatalf("expected an absolute-path key to be confined and rejected")
    }
}
