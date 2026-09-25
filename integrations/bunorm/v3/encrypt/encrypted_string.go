package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
)

const redactedPlaceholder = "<redacted>"

/* EncryptedColumn marks every encrypted column type, the generic instantiations included. A consumer such as the audit trail's auto-redaction matches this interface, since each generic instantiation is a distinct reflect.Type. */
type EncryptedColumn interface {
    encryptedColumn()
}

type EncryptedString string

func (instance EncryptedString) encryptedColumn() {}

func (instance EncryptedString) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedString) GoString() string {
    return redactedPlaceholder
}

/* Format answers every verb that reaches it, the numeric ones included, with the redacted rendering. %p and %w never reach it, since fmt prints the operand by reflection first, and a value in an unexported field of a struct rendered with %v is walked by reflection with no method called. */
func (instance EncryptedString) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes, so a round-tripped document cannot seal the placeholder over the secret. Any other string is the plaintext, and a json null leaves the value untouched. */
func (instance *EncryptedString) UnmarshalJSON(data []byte) error {
    return unmarshalEncryptedJson(instance, data)
}

func (instance EncryptedString) Value() (driver.Value, error) {
    cipherInstance, cipherErr := cipherByName(defaultCipherName)
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.Encrypt(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedString) Scan(source any) error {
    raw, isNull, decodeErr := scanRaw(source)
    if nil != decodeErr {
        return decodeErr
    }

    if true == isNull {
        *instance = ""
        return nil
    }

    cipherInstance, cipherErr := cipherByName(defaultCipherName)
    if nil != cipherErr {
        return cipherErr
    }

    plaintext, plaintextErr := cipherInstance.Decrypt(raw)
    if nil != plaintextErr {
        return plaintextErr
    }

    *instance = EncryptedString(plaintext)

    return nil
}

func scanRaw(source any) (string, bool, error) {
    if nil == source {
        return "", true, nil
    }

    switch typed := source.(type) {
    case string:
        return typed, false, nil
    case []byte:
        return string(typed), false, nil
    default:
        return "", false, exception.NewError("encrypted string scan received an unsupported type", nil, nil)
    }
}

func unmarshalEncryptedJson[T ~string](instance *T, data []byte) error {
    decoded, present, decodeErr := decodeEncryptedJson(data, instance)
    if nil != decodeErr {
        return decodeErr
    }

    if true == present {
        *instance = T(decoded)
    }

    return nil
}

/* columnTypeOf names the column type of a pointer receiver for a refusal; it is asked in the refusal branches alone, so a successful decode pays nothing for it. */
func columnTypeOf(column any) string {
    return reflect.TypeOf(column).Elem().String()
}

/* decodeEncryptedJson answers the decoded plaintext and whether one was present, a json null carrying none. The redaction placeholder is refused, naming the column type. */
func decodeEncryptedJson(data []byte, column any) (string, bool, error) {
    if "null" == string(data) {
        return "", false, nil
    }

    var decoded string
    if unmarshalErr := json.Unmarshal(data, &decoded); nil != unmarshalErr {
        return "", false, exception.NewError("encrypted column json value is not a string", map[string]any{"type": columnTypeOf(column)}, unmarshalErr)
    }

    if redactedPlaceholder == decoded {
        return "", false, exception.NewError(
            "a redacted encrypted value cannot be decoded back into an encrypted column; MarshalJSON removed the plaintext",
            map[string]any{"type": columnTypeOf(column)},
            nil,
        )
    }

    return decoded, true, nil
}

func errCipherNotConfigured() error {
    return exception.NewError("encryption cipher is not configured; call encrypt.UseCipher(...) first", nil, nil)
}

var _ driver.Valuer = EncryptedString("")
var _ json.Unmarshaler = (*EncryptedString)(nil)
var _ EncryptedColumn = EncryptedString("")
