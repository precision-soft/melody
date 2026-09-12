//go:build melody_env_embedded

package application

import (
    "io/fs"
    "testing"
)

type testTypedNilFs struct{}

func (instance *testTypedNilFs) Open(name string) (fs.File, error) {
    return nil, fs.ErrNotExist
}

func TestNewEnvironmentSource_RefusesATypedNilFs(t *testing.T) {
    defer func() {
        if recoveredValue := recover(); nil == recoveredValue {
            t.Fatalf("expected the typed-nil embedded fs to be refused")
        }
    }()

    var typedNilFs *testTypedNilFs

    _ = newEnvironmentSource("/tmp/melody", typedNilFs)
}
