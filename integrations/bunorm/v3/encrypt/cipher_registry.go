package encrypt

import (
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

/* CipherRef binds a column type to a named cipher through the type system: a zero-size marker type implements CipherName() and parameterizes EncryptedStringFor / EncryptedDeterministicStringFor, so the binding travels with the Go type — the only channel available, since database/sql gives Value() and Scan() no context. */
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

/* UseCipherNamed installs a named cipher — one key compartment — for columns bound through a CipherRef marker. Each named cipher owns its KeyProvider, so compartments stay isolated: the "crm" cipher can never decrypt a "billing" ciphertext, unlike merging every key into one provider where either context can read the other's rows. */
func UseCipherNamed(name string, cipherInstance Cipher) {
    if "" == name {
        exception.Panic(exception.NewError("named cipher name is empty; use UseCipher for the default cipher", nil, nil))
    }

    storeCipher(name, cipherInstance)
}

/* storeCipher installs, replaces or — for a bare nil — uninstalls a registry entry; the bare nil is the documented deinstall door test binaries reset compartments with. A TYPED nil is a different thing entirely: it is a wiring error (a resolution that failed and was installed anyway), it is not a deinstall request, and stored it would be handed out by cipherByName with a nil error and dereferenced inside database/sql at the first column write. It is refused here, at the boot-time door whose caller can be named. */
func storeCipher(name string, cipherInstance Cipher) {
    if nil != cipherInstance {
        reflected := reflect.ValueOf(cipherInstance)
        if reflect.Ptr == reflected.Kind() && true == reflected.IsNil() {
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
