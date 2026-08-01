package validation

import (
    "encoding/json"
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* @info the constructor copies on the way in and Context copies on the way out to keep the map private; ToExceptionError handed out the live map, so a consumer mutating the exception's context mutated the validation error through it */
func TestValidationError_ToExceptionErrorDoesNotAliasTheContext(t *testing.T) {
    validationError := NewValidationError("field", "message", "code", map[string]any{"actual": 42})

    exceptionErr := validationError.ToExceptionError()

    melodyError, ok := exceptionErr.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", exceptionErr)
    }

    nested, ok := melodyError.Context()["context"].(map[string]any)
    if false == ok {
        t.Fatalf("expected the nested validation context to be present")
    }

    nested["actual"] = "mutated"

    if 42 != validationError.Context()["actual"] {
        t.Fatalf("external mutation reached the validation error's internal context: %v", validationError.Context()["actual"])
    }
}

/* @info decizie user 08-01: the collection marshals as the array it is, each element through its own marshaler, so a log record carrying it says the same thing the http response body says — the flattened Error() string was the only form the log ever saw */
func TestValidationErrors_MarshalJsonRendersTheStructure(t *testing.T) {
    validationErrors := ValidationErrors{
        NewValidationError("name", "is required", "required", nil),
        NewValidationError("age", "must be numeric", "numeric", map[string]any{"actual": "abc"}),
    }

    encoded, marshalErr := json.Marshal(validationErrors)
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    var decoded []map[string]any
    if unmarshalErr := json.Unmarshal(encoded, &decoded); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    if 2 != len(decoded) {
        t.Fatalf("expected two structured entries, got %d", len(decoded))
    }

    if "name" != decoded[0]["field"] || "is required" != decoded[0]["message"] || "required" != decoded[0]["code"] {
        t.Fatalf("expected the first entry structured, got %v", decoded[0])
    }

    if "abc" != decoded[1]["context"].(map[string]any)["actual"] {
        t.Fatalf("expected the second entry's context, got %v", decoded[1])
    }
}
