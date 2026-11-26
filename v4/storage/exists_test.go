package storage

import (
    "errors"
    "os"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

func TestExists_ReadsStat(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()
    boundStorage := For(runtimeInstance, local)

    if putErr := boundStorage.Put("prefix/object.bin", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    for key, expected := range map[string]bool{
        "prefix/object.bin":       true,
        "absent.bin":              false,
        "prefix":                  false,
        "prefix/object.bin/child": false,
    } {
        exists, existsErr := Exists(boundStorage, key)
        if nil != existsErr || expected != exists {
            t.Fatalf("%q: expected %v and no error, got %v %v", key, expected, exists, existsErr)
        }
    }

    exists, existsErr := Exists(boundStorage, "../prefix/object.bin")
    if true == exists || nil == existsErr || false == strings.Contains(existsErr.Error(), `".." segment`) {
        t.Fatalf("expected a parent segment answered false beside its refusal, got %v %v", exists, existsErr)
    }

    if removeErr := os.RemoveAll(base); nil != removeErr {
        t.Fatalf("remove: %v", removeErr)
    }

    if exists, existsErr := Exists(boundStorage, "prefix/object.bin"); nil != existsErr || true == exists {
        t.Fatalf("expected a base directory that is gone to hold nothing, got %v %v", exists, existsErr)
    }
}

var existsScriptedStatErr = errors.New("the storage could not be asked")

type existsScriptedStorage struct {
    boundRecordingStorage
    statErr error
}

func (instance *existsScriptedStorage) Stat(runtimeInstance containercontract.Runtime, key string) (storagecontract.ObjectInfo, error) {
    instance.record("Stat", runtimeInstance, key)

    return storagecontract.ObjectInfo{}, instance.statErr
}

func TestExists_AnswersAFailureOfStatAsAFailure(t *testing.T) {
    runtimeInstance := testRuntime()
    scriptedStorage := &existsScriptedStorage{statErr: existsScriptedStatErr}

    exists, existsErr := Exists(For(runtimeInstance, scriptedStorage), "object.bin")
    if true == exists || existsScriptedStatErr != existsErr {
        t.Fatalf("expected false beside the error Stat answered, by identity, got %v %v", exists, existsErr)
    }

    if 1 != len(scriptedStorage.callList) || "Stat" != scriptedStorage.callList[0].door || runtimeInstance != scriptedStorage.callList[0].runtimeInstance {
        t.Fatalf("expected one Stat under the bound runtime, got %v", scriptedStorage.callList)
    }

    for _, absence := range []error{storagecontract.ErrObjectNotFound, storagecontract.ErrKeyIsDirectory} {
        scriptedStorage.statErr = errors.Join(errors.New("wrapped"), absence)

        if exists, existsErr := Exists(For(runtimeInstance, scriptedStorage), "object.bin"); true == exists || nil != existsErr {
            t.Fatalf("%v: expected false and no error, got %v %v", absence, exists, existsErr)
        }
    }

    scriptedStorage.statErr = nil
    if exists, existsErr := Exists(For(runtimeInstance, scriptedStorage), "object.bin"); false == exists || nil != existsErr {
        t.Fatalf("expected true for a Stat that answered, got %v %v", exists, existsErr)
    }
}
