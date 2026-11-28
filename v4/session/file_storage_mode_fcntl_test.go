//go:build unix

package session

import (
    "os"
    "testing"
)

func TestNewFileStorage_RefusesAReadOnlyHandle(t *testing.T) {
    fileInstance := openSeededSessionFile(t, os.O_RDONLY)

    storage, storageErr := NewFileStorageOverFile(fileInstance, FileStorageConfig{})
    if nil == storageErr {
        t.Fatalf("expected a read-only handle to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over a read-only handle")
    }

    if "session storage file is opened read-only" != storageErr.Error() {
        t.Fatalf("expected the read-only refusal, got %q", storageErr.Error())
    }
}
