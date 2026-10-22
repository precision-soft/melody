//go:build unix

package storage

import (
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"
)

/* a named pipe under a key is not an object: Get refuses it at once, where a blocking open waited for a writer forever, and Exists answers false */
func TestLocalStorage_ANamedPipeUnderAKeyIsNotAnObject(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base)
    runtimeInstance := testRuntime()

    if mkfifoErr := syscall.Mkfifo(filepath.Join(base, "pipe"), 0o600); nil != mkfifoErr {
        t.Skipf("mkfifo is not available here: %v", mkfifoErr)
    }

    answered := make(chan error, 1)
    go func() {
        reader, getErr := local.Get(runtimeInstance, "pipe")
        if nil != reader {
            _ = reader.Close()
        }
        answered <- getErr
    }()

    select {
    case getErr := <-answered:
        if nil == getErr || false == strings.Contains(getErr.Error(), "not a regular object") {
            t.Fatalf("expected the pipe refused, got %v", getErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected Get to answer at once on a named pipe")
    }

    if exists, existsErr := local.Exists(runtimeInstance, "pipe"); nil != existsErr || true == exists {
        t.Fatalf("expected the pipe not to exist as an object, got %v %v", exists, existsErr)
    }
}
