package validation

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
)

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

func TestValidationError_AnErrorWithoutContextAnswersNothingRatherThanAnEmptyMap(t *testing.T) {
    validationError := NewValidationError("field", "message", "code", nil)

    if nil != validationError.Context() {
        t.Fatalf("expected no context rather than an empty map, got %#v", validationError.Context())
    }

    payload, marshalErr := json.Marshal(validationError)
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "context") {
        t.Fatalf("expected the absent context to be omitted from the payload, got %s", payload)
    }
}

func TestValidationErrors_AnEmptyCollectionRendersAsNothing(t *testing.T) {
    var empty ValidationErrors

    if "" != empty.Error() {
        t.Fatalf("expected an empty collection to render as nothing, got %q", empty.Error())
    }

    if "" != (ValidationErrors{}).Error() {
        t.Fatalf("expected an empty non-nil collection to render as nothing, got %q", ValidationErrors{}.Error())
    }
}

func TestValidationErrors_RenderTheirMembersInAStableOrder(t *testing.T) {
    forward := ValidationErrors{
        NewValidationError("zebra", "last", "code", nil),
        NewValidationError("alpha", "first", "code", nil),
    }

    reversed := ValidationErrors{
        NewValidationError("alpha", "first", "code", nil),
        NewValidationError("zebra", "last", "code", nil),
    }

    if forward.Error() != reversed.Error() {
        t.Fatalf("expected the rendering to be order-independent, got %q and %q", forward.Error(), reversed.Error())
    }

    if false == strings.HasPrefix(forward.Error(), "alpha: first") {
        t.Fatalf("expected the rendering to be sorted, got %q", forward.Error())
    }
}
