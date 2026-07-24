package serializer

import (
    "errors"
    "strings"
    "testing"

    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

type serializerTestSerializer struct {
    name string
}

func (instance *serializerTestSerializer) ContentType() string {
    return MimeApplicationJson + "; charset=utf-8"
}

func (instance *serializerTestSerializer) Serialize(value any) ([]byte, error) {
    return []byte(instance.name), nil
}

func (instance *serializerTestSerializer) Deserialize(payload []byte, target any) error {
    return nil
}

var _ serializercontract.Serializer = (*serializerTestSerializer)(nil)

func TestNewSerializerManager_PanicsOnEmptyMimeKey(t *testing.T) {
    _, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            "   ": &serializerTestSerializer{name: "x"},
        },
    )

    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestSerializerManager_Get_NormalizesMime(t *testing.T) {
    manager, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            "application/json": &serializerTestSerializer{name: "json"},
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    serializerInstance, exists := manager.Get("application/json; charset=utf-8")
    if false == exists {
        t.Fatalf("expected serializer")
    }
    if nil == serializerInstance {
        t.Fatalf("expected non-nil serializer")
    }
}

func TestSerializerManager_ResolveByAcceptHeader_DefaultsToApplicationJson(t *testing.T) {
    manager, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            MimeApplicationJson: &serializerTestSerializer{name: "json"},
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    serializerInstance, err := manager.ResolveByAcceptHeader("")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if nil == serializerInstance {
        t.Fatalf("expected serializer")
    }

    if MimeApplicationJson != normalizeMime(serializerInstance.ContentType()) {
        t.Fatalf("unexpected serializer content type: %s", serializerInstance.ContentType())
    }
}

type testSerializerPlain struct{}

func (instance *testSerializerPlain) ContentType() string {
    return "text/plain"
}

func (instance *testSerializerPlain) Serialize(payload any) ([]byte, error) {
    return []byte("plain"), nil
}

func (instance *testSerializerPlain) Deserialize(data []byte, target any) error {
    return nil
}

var _ serializercontract.Serializer = (*testSerializerPlain)(nil)

type testSerializerHtml struct{}

func (instance *testSerializerHtml) ContentType() string {
    return "text/html"
}

func (instance *testSerializerHtml) Serialize(payload any) ([]byte, error) {
    return []byte("html"), nil
}

func (instance *testSerializerHtml) Deserialize(data []byte, target any) error {
    return nil
}

var _ serializercontract.Serializer = (*testSerializerHtml)(nil)

func TestSerializerManager_ResolveByAcceptHeader_WildcardSubtype_SelectsLexicalFirst(t *testing.T) {
    manager, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            "text/plain": &testSerializerPlain{},
            "text/html":  &testSerializerHtml{},
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    resolved, err := manager.ResolveByAcceptHeader("text/*")
    if nil != err {
        t.Fatalf("unexpected error")
    }

    if "text/html" != normalizeMime(resolved.ContentType()) {
        t.Fatalf("expected lexical first content type to win for wildcard subtype")
    }
}

/* @info each available type takes the quality of the most specific range covering it, so an exact range wins over a wildcard regardless of header order, and a q of 0 refuses rather than being ignored */
func TestResolveByAcceptHeader_MostSpecificRangeWins(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
        MimeTextPlain:       NewPlainTextSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    for _, testCase := range []struct {
        acceptHeader string
        expectedMime string
    }{
        {"*/*, text/plain", MimeTextPlain},
        {"text/plain, */*", MimeTextPlain},
        {"*/*", MimeApplicationJson},
        {"text/*", MimeTextPlain},
        {"application/json;q=0.2, text/plain;q=0.8", MimeTextPlain},
        {"*/*;q=0.9, text/plain;q=0.1", MimeApplicationJson},
    } {
        resolved, resolveErr := manager.ResolveByAcceptHeader(testCase.acceptHeader)
        if nil != resolveErr {
            t.Fatalf("accept %q: unexpected error %v", testCase.acceptHeader, resolveErr)
        }

        if false == strings.HasPrefix(resolved.ContentType(), testCase.expectedMime) {
            t.Fatalf("accept %q: expected %s, got %s", testCase.acceptHeader, testCase.expectedMime, resolved.ContentType())
        }
    }
}

func TestResolveByAcceptHeader_ExplicitRefusalIsNotAcceptable(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    for _, acceptHeader := range []string{
        "application/xml, application/json;q=0",
        "application/json;q=0",
        "*/*;q=0",
    } {
        _, resolveErr := manager.ResolveByAcceptHeader(acceptHeader)
        if false == errors.Is(resolveErr, ErrNotAcceptable) {
            t.Fatalf("accept %q: expected a not-acceptable error, got %v", acceptHeader, resolveErr)
        }
    }
}

/* @info a header that simply matches nothing available is not a refusal: the default representation is still served, which is what every client sending a narrow accept header against this framework relies on */
func TestResolveByAcceptHeader_UnmatchedHeaderStillFallsBackToJson(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    resolved, resolveErr := manager.ResolveByAcceptHeader("application/xml")
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if false == strings.HasPrefix(resolved.ContentType(), MimeApplicationJson) {
        t.Fatalf("expected the default representation, got %s", resolved.ContentType())
    }
}
