package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"

    "github.com/precision-soft/melody/v3/exception"
)

const redactedPlaceholder = "<redacted>"

/* EncryptedColumn marks every encrypted column type — the two default-cipher forms and every instantiation of the two compartment-bound generic forms. A consumer that must recognise "a value of an encrypted column type" (the audit trail's auto-redaction) matches this interface rather than comparing types by identity, because each generic instantiation is a distinct reflect.Type and an identity list could never enumerate them. */
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

/* Format redacts under the numeric verbs (%d %o %b %c %U) that fmt routes through neither Stringer nor GoStringer — it consults those only for %v %s %q %x %X and %#v — so a numeric verb would otherwise print the underlying string through the badverb form (`%!d(encrypt.EncryptedString=<plaintext>)`), carrying the secret. Every verb is answered with the same redacted rendering, the way the encrypt key provider closes the same gap. */
func (instance EncryptedString) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON is the read side of the redaction: a document MarshalJSON produced carries the placeholder where the plaintext was, and decoding it back into the column used to store the placeholder as the value, so the next Value() sealed "<redacted>" in place of the secret with no error anywhere on the way. The placeholder is refused by name; any other string is the plaintext the application typed, and a json null leaves the value untouched. */
func (instance *EncryptedString) UnmarshalJSON(data []byte) error {
    decoded, present, decodeErr := decodeEncryptedJson(data, fmt.Sprintf("%T", *instance))
    if nil != decodeErr {
        return decodeErr
    }

    if true == present {
        *instance = EncryptedString(decoded)
    }

    return nil
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

/* decodeEncryptedJson is the shared read side of the four column types' MarshalJSON. It answers the decoded plaintext and whether one was present: a json null is the no-op encoding/json asks of every Unmarshaler, so nothing is present and nothing is an error. The redaction placeholder is refused rather than stored, because a value equal to it can only have come from a document this package redacted — an application that round-trips such a document would otherwise seal the placeholder over its own secret in silence. The refusal names the column type so the wiring that decoded the document is the one reported. */
func decodeEncryptedJson(data []byte, columnType string) (string, bool, error) {
    if "null" == string(data) {
        return "", false, nil
    }

    var decoded string
    if unmarshalErr := json.Unmarshal(data, &decoded); nil != unmarshalErr {
        return "", false, exception.NewError("encrypted column json value is not a string", map[string]any{"type": columnType}, unmarshalErr)
    }

    if redactedPlaceholder == decoded {
        return "", false, exception.NewError(
            "a redacted encrypted value cannot be decoded back into an encrypted column; MarshalJSON removed the plaintext",
            map[string]any{"type": columnType},
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
