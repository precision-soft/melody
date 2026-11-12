package encrypt

import (
    "reflect"
    "regexp"
    "sync"

    "github.com/precision-soft/melody/v4/exception"
)

/* CipherRef binds a column type to a named cipher through the type system: a zero-size marker type implementing CipherName parameterizes EncryptedStringFor and EncryptedDeterministicStringFor, since database/sql gives Value and Scan no context. */
type CipherRef interface {
    CipherName() string
}

/* SealBindingRef is a CipherRef whose columns are sealed bound to a label, <table>.<column>: the label is authenticated in each seal and taken into the deterministic nonce, so a seal opens through a column of this marker alone and equal values of two bound columns are not equal seals. Every such marker is declared in ModuleConfig.SealBindings; an undeclared one is refused at its column's first Value or Scan. */
type SealBindingRef interface {
    CipherRef

    SealBinding() string
}

/* DefaultCipherRef, embedded by value in a marker type, selects the cipher installed as ModuleConfig.Cipher, so a bound column can use the default cipher; any other marker naming the empty compartment is refused. */
type DefaultCipherRef struct{}

func (instance DefaultCipherRef) CipherName() string {
    return defaultCipherName
}

func (instance DefaultCipherRef) defaultCipherRef() {}

/* defaultCipherSelector is promoted by embedding DefaultCipherRef, and is the one way a marker may name the empty compartment */
type defaultCipherSelector interface {
    defaultCipherRef()
}

var sealBindingRefType = reflect.TypeOf((*SealBindingRef)(nil)).Elem()

var defaultCipherRefPointerType = reflect.TypeOf((*DefaultCipherRef)(nil))

/* markerShapeDefect answers why a marker type cannot carry a binding, empty when it can: a marker holds no state, since the column reads the zero value of its type; a SealBinding on the pointer receiver would leave the value type unbound without a word; and DefaultCipherRef embedded by pointer promotes its methods over a nil pointer */
func markerShapeDefect(marker reflect.Type) string {
    if _, isBound := reflect.Zero(marker).Interface().(SealBindingRef); false == isBound && true == reflect.PointerTo(marker).Implements(sealBindingRefType) {
        return "cipher reference declares SealBinding on its pointer receiver; a marker's methods must have value receivers"
    }

    if reflect.Struct == marker.Kind() {
        for index := 0; index < marker.NumField(); index++ {
            if field := marker.Field(index); true == field.Anonymous && defaultCipherRefPointerType == field.Type {
                return "cipher reference embeds DefaultCipherRef by pointer; embed it by value"
            }
        }
    }

    if _, isBound := reflect.Zero(marker).Interface().(SealBindingRef); true == isBound && 0 != marker.Size() {
        return "cipher reference of a seal binding holds fields; a marker must be a zero-size value type, since its column reads the zero value"
    }

    return ""
}

/* defaultCipherName is the entry ModuleConfig.Cipher is installed under; a context's name is never empty */
const defaultCipherName = ""

/* sealBindingLabelPattern is the label grammar, one canonical spelling per column, so two spellings of one column cannot silently be two bindings */
var sealBindingLabelPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}\.[A-Za-z0-9_]{1,64}$`)

var sealBindingRegistryMutex sync.RWMutex
var sealBindingRegistry = map[reflect.Type]sealBinding{}

func sealBindingOf(marker reflect.Type) (sealBinding, bool) {
    sealBindingRegistryMutex.RLock()
    defer sealBindingRegistryMutex.RUnlock()

    binding, declared := sealBindingRegistry[marker]

    return binding, declared
}

/* a process-wide table, since database/sql hands Value and Scan no context */
var cipherRegistryMutex sync.RWMutex
var cipherRegistry = map[string]Cipher{}

/* replaceRegistries installs one registration's whole state: the cipher table and the binding table are replaced, never merged, so a later application in the process keeps no cipher name and no marker of an earlier one; validateConfig has refused a nil cipher before */
func replaceRegistries(ciphers map[string]Cipher, bindings map[reflect.Type]sealBinding) {
    cipherRegistryMutex.Lock()
    defer cipherRegistryMutex.Unlock()

    sealBindingRegistryMutex.Lock()
    defer sealBindingRegistryMutex.Unlock()

    cipherRegistry = ciphers
    sealBindingRegistry = bindings
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
