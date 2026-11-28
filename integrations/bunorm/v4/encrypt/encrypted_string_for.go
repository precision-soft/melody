package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"

    "github.com/precision-soft/melody/v4/exception"
)

/* EncryptedStringFor is EncryptedString bound to the named cipher selected by the CipherReference marker R, so a multi-context binary keeps one key compartment per context:

    type CrmCipher struct{}

    func (instance CrmCipher) CipherName() string {
        return "crm"
    }

    type Customer struct {
        Iban encrypt.EncryptedStringFor[CrmCipher] `bun:"iban"`
    }

Install the compartment by registering the encrypt module with a context named "crm" and its Cipher. Each named cipher owns its KeyProvider: key rotation inside the compartment keeps working through the key id embedded in the ciphertext, and a column of one compartment cannot decrypt another's as long as each provider holds keys of its own, since the name is not bound into the ciphertext. A marker that is also a SealBindingReference binds its column's seals to a label, which is; the README's "Binding a seal to its column" section says how.
*/
type EncryptedStringFor[R CipherReference] string

func (instance EncryptedStringFor[R]) encryptedColumn() {}

func (instance EncryptedStringFor[R]) String() string {
    return redactedPlaceholder
}

/* Format answers every verb that reaches it, the numeric ones included, with the redacted rendering; its limits are on EncryptedString.Format. */
func (instance EncryptedStringFor[R]) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes and decodes any other string, for the reason on EncryptedString.UnmarshalJSON. */
func (instance *EncryptedStringFor[R]) UnmarshalJSON(data []byte) error {
    return unmarshalEncryptedJson(instance, data)
}

func (instance EncryptedStringFor[R]) Value() (driver.Value, error) {
    return sealReferenceColumnValue[R](string(instance), false)
}

func (instance *EncryptedStringFor[R]) Scan(source any) error {
    raw, isNull, decodeErr := scanRaw(source)
    if nil != decodeErr {
        return decodeErr
    }

    if true == isNull {
        *instance = ""
        return nil
    }

    plaintext, plaintextErr := openReferenceColumnValue[R](raw)
    if nil != plaintextErr {
        return plaintextErr
    }

    *instance = EncryptedStringFor[R](plaintext)

    return nil
}

/* referenceColumn resolves the cipher a marker type selects, instantiating the marker as its zero value, and the binding it declares, nil for a marker without one. An empty CipherName is refused unless the marker embeds DefaultCipherReference, since the empty name is the default cipher's entry and would hand a compartment-bound column the default key by accident. A marker with a binding is refused when it was not declared, and when its cipher does not carry the binding door. */
func referenceColumn[R CipherReference]() (Cipher, *sealBinding, error) {
    var reference R

    markerName := reflect.TypeOf((*R)(nil)).Elem().String()

    /* a pointer-form marker's zero value is nil, and an interface marker, CipherReference itself included, has no zero value to call, so either CipherName call would panic inside database/sql; both are answered as an error naming the marker */
    if reflected := reflect.ValueOf(any(reference)); false == reflected.IsValid() || (reflect.Pointer == reflected.Kind() && true == reflected.IsNil()) {
        return nil, nil, exception.NewError(
            "cipher reference is a pointer or interface type; a CipherReference must be a zero-size value type",
            map[string]any{"cipherReference": markerName},
            nil,
        )
    }

    if defect := markerShapeDefect(reflect.TypeOf(reference)); "" != defect {
        return nil, nil, exception.NewError(defect, map[string]any{"cipherReference": markerName}, nil)
    }

    name := reference.CipherName()
    if _, selectsDefault := any(reference).(defaultCipherSelector); "" == name && false == selectsDefault {
        return nil, nil, exception.NewError(
            "cipher reference name is empty; a CipherReference must name a compartment registered as an encrypt module context, or embed DefaultCipherReference",
            map[string]any{"cipherReference": markerName},
            nil,
        )
    }

    cipherInstance, cipherErr := cipherByName(name)
    if nil != cipherErr {
        return nil, nil, cipherErr
    }

    if _, isBound := any(reference).(SealBindingReference); false == isBound {
        return cipherInstance, nil, nil
    }

    binding, declared := sealBindingOf(reflect.TypeOf(reference))
    if false == declared {
        return nil, nil, exception.NewError(
            "cipher reference declares a seal binding that is not declared in ModuleConfig.SealBindings",
            map[string]any{"cipherReference": markerName, "field": "ModuleConfig.SealBindings"},
            nil,
        )
    }

    if _, carriesBinding := cipherInstance.(bindingSealer); false == carriesBinding {
        return nil, nil, errBindingThroughForeignCipher(markerName)
    }

    return cipherInstance, &binding, nil
}

func errBindingThroughForeignCipher(markerName string) error {
    return exception.NewError(
        "a bound column needs the package cipher; a cipher implemented outside the package, a wrapper included, cannot bind a seal",
        map[string]any{"cipherReference": markerName},
        nil,
    )
}

/* sealReferenceColumnValue is the Value of both column types of a marker: a bound marker seals gcm2 under its binding through the binding door, any other seals as the plain column types do */
func sealReferenceColumnValue[R CipherReference](plaintext string, deterministic bool) (driver.Value, error) {
    cipherInstance, binding, resolveErr := referenceColumn[R]()
    if nil != resolveErr {
        return nil, resolveErr
    }

    var encoded string
    var encryptErr error
    if nil != binding {
        encoded, encryptErr = cipherInstance.(bindingSealer).sealBoundValue(plaintext, *binding, deterministic)
    } else {
        encoded, encryptErr = sealColumnValue(cipherInstance, plaintext, deterministic)
    }

    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

/* openReferenceColumnValue is the Scan of both column types of a marker, after a NULL is answered */
func openReferenceColumnValue[R CipherReference](raw string) (string, error) {
    cipherInstance, binding, resolveErr := referenceColumn[R]()
    if nil != resolveErr {
        return "", resolveErr
    }

    if nil == binding {
        return cipherInstance.Decrypt(raw)
    }

    opened, openErr := cipherInstance.(bindingSealer).openStored(raw, *binding)
    if nil != openErr {
        return "", openErr
    }

    return opened.plaintext, nil
}
