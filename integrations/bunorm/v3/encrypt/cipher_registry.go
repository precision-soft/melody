package encrypt

import (
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

/* CipherRef binds a column type to a named cipher through the type system: a zero-size marker type implementing CipherName parameterizes EncryptedStringFor and EncryptedDeterministicStringFor, since database/sql gives Value and Scan no context. */
type CipherRef interface {
    CipherName() string
}

/* defaultCipherName is the reserved registry entry behind UseCipher; named ciphers must use a non-empty name. */
const defaultCipherName = ""

var cipherRegistryMutex sync.RWMutex
var cipherRegistry = map[string]Cipher{}

/* UseCipher installs the process-wide default cipher used by EncryptedString and EncryptedDeterministicString. A single-database application needs nothing else; a binary with several key compartments installs one named cipher per compartment with UseCipherNamed and binds columns via EncryptedStringFor. */
func UseCipher(cipherInstance Cipher) {
    storeCipher(defaultCipherName, cipherInstance)
}

/* UseCipherNamed installs a named cipher — one key compartment — for columns bound through a CipherRef marker. Each named cipher owns its KeyProvider, so compartments are isolated as long as each provider holds keys of its own: the name selects the registry entry and is not bound into the ciphertext, so two compartments holding the same key under the same id decrypt each other. Merging every key into one provider loses even that, since either context can read the other's rows. */
func UseCipherNamed(name string, cipherInstance Cipher) {
    if "" == name {
        exception.Panic(exception.NewError("named cipher name is empty; use UseCipher for the default cipher", nil, nil))
    }

    storeCipher(name, cipherInstance)
}

/* storeCipher installs, replaces or, for a bare nil, uninstalls a registry entry. A typed nil is a wiring error and is refused here, since cipherByName would hand it out and database/sql would dereference it at the first column write. */
func storeCipher(name string, cipherInstance Cipher) {
    if nil != cipherInstance {
        reflected := reflect.ValueOf(cipherInstance)
        if reflect.Pointer == reflected.Kind() && true == reflected.IsNil() {
            exception.Panic(exception.NewError("cipher instance is a typed nil", map[string]any{"cipherName": name}, nil))
        }
    }

    cipherRegistryMutex.Lock()
    defer cipherRegistryMutex.Unlock()

    if nil == cipherInstance {
        delete(cipherRegistry, name)
        return
    }

    cipherRegistry[name] = cipherInstance
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
        "encryption cipher is not configured for this name; call encrypt.UseCipherNamed(...) first",
        map[string]any{
            "cipherName": name,
        },
        nil,
    )
}
