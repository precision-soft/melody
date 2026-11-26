//go:build unix

package storage

import (
    "errors"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"

    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

func TestLocalStorage_GetRefusesANamedPipe(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if mkfifoErr := syscall.Mkfifo(filepath.Join(base, "pipe"), 0o600); nil != mkfifoErr {
        t.Skipf("mkfifo is not available here: %v", mkfifoErr)
    }

    answered := make(chan error, 1)
    go func() {
        object, getErr := local.Get(runtimeInstance, "pipe", storagecontract.GetOptions{})
        if nil != object.Body {
            _ = object.Body.Close()
            getErr = errors.New("a body beside the refusal")
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

    if exists, existsErr := Exists(For(runtimeInstance, local), "pipe"); nil == existsErr || false == strings.Contains(existsErr.Error(), "not a regular object") || true == exists {
        t.Fatalf("expected the pipe refused as no regular object, got %v %v", exists, existsErr)
    }
}

func TestLocalStorage_StatRefusesANamedPipeWithoutOpeningIt(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if mkfifoErr := syscall.Mkfifo(filepath.Join(base, "pipe"), 0o600); nil != mkfifoErr {
        t.Skipf("mkfifo is not available here: %v", mkfifoErr)
    }

    answered := make(chan error, 1)
    go func() {
        _, statErr := local.Stat(runtimeInstance, "pipe")
        answered <- statErr
    }()

    select {
    case statErr := <-answered:
        if nil == statErr || false == strings.Contains(statErr.Error(), "not a regular object") {
            t.Fatalf("expected the pipe refused, got %v", statErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected Stat to answer at once on a named pipe")
    }
}
