package encrypt

import (
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v4/exception"
)

/* CipherRef binds a column type to a named cipher through the type system: a zero-size marker type implementing CipherName parameterizes EncryptedStringFor and EncryptedDeterministicStringFor, since database/sql gives Value and Scan no context. */
type CipherRef interface {
    CipherName() string
}

/* defaultCipherName is the entry ModuleConfig.Cipher is installed under; a context's name is never empty */
const defaultCipherName = ""

/* a process-wide table, since database/sql hands Value and Scan no context */
var cipherRegistryMutex sync.RWMutex
var cipherRegistry = map[string]Cipher{}

/* storeCipher replaces an earlier entry, so a later application in the process installs its own cipher; a nil cipher is refused, since database/sql would dereference it at the first column write */
func storeCipher(name string, cipherInstance Cipher) {
    if true == isNilCipher(cipherInstance) {
        exception.Panic(exception.NewError("cipher instance is nil", map[string]any{"cipherName": name}, nil))
    }

    cipherRegistryMutex.Lock()
    defer cipherRegistryMutex.Unlock()

    cipherRegistry[name] = cipherInstance
}

func isNilCipher(cipherInstance Cipher) bool {
    if nil == cipherInstance {
        return true
    }

    reflected := reflect.ValueOf(cipherInstance)
    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

func cipherByName(name string) (Cipher, error) {
    cipherRegistryMutex.RLock()
    defer cipherRegistryMutex.RUnlock()

    cipherInstance, exists := cipherRegistry[name]
    if false == exists || nil == cipherInstance {
        if defaultCipherName == name {
            return nil, errCipherNotConfigured()
        }

        return nil, errNamedCipherNotConfigured(name)
    }

    return cipherInstance, nil
}

func errNamedCipherNotConfigured(name string) error {
    return exception.NewError(
        "no cipher is installed under this name; register the encrypt module with a context of this name and its Cipher",
        map[string]any{
            "cipherName": name,
        },
        nil,
    )
}
