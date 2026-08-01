package serializer

import (
    "errors"
    "strings"
    "testing"

    serializercontract "github.com/precision-soft/melody/serializer/contract"
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

/* @info a typed nil passes the plain nil comparison, would be stored as a live serializer and would dereference its nil receiver on the first request the negotiation routes to it — the constructor refuses it with the same error the untyped nil gets */
func TestNewSerializerManager_RefusesATypedNilSerializer(t *testing.T) {
    var typedNil *JsonSerializer

    _, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            MimeApplicationJson: typedNil,
        },
    )

    if nil == err {
        t.Fatalf("expected the typed-nil serializer to be refused")
    }

    if false == strings.Contains(err.Error(), "serializer instance is nil") {
        t.Fatalf("expected the nil-instance refusal, got: %v", err)
    }
}

/* @info two spellings collapsing into one normalized mime are refused at construction: map iteration order would decide the surviving serializer, so the winner would change from one boot to the next with the loser dropped silently */
func TestNewSerializerManager_RefusesCollidingMimeKeys(t *testing.T) {
    _, err := NewSerializerManager(
        map[string]serializercontract.Serializer{
            "application/json":                &serializerTestSerializer{name: "bare"},
            "Application/JSON; charset=utf-8": &serializerTestSerializer{name: "spelled"},
        },
    )

    if nil == err {
        t.Fatalf("expected the colliding mime keys to be refused")
    }

    if false == strings.Contains(err.Error(), "collide") {
        t.Fatalf("expected the collision refusal, got: %v", err)
    }
}

/* @info an empty accept header means the client takes anything, and a header matching nothing available still receives the default representation: a manager deliberately configured without json serves its first configured serializer in lexical mime order instead of refusing every such request while a serializer sits configured beside it */
func TestResolveByAcceptHeader_WithoutJsonFallsBackToTheFirstConfiguredSerializer(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        "text/plain": &testSerializerPlain{},
        "text/html":  &testSerializerHtml{},
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    for _, acceptHeader := range []string{"", "application/xml"} {
        resolved, resolveErr := manager.ResolveByAcceptHeader(acceptHeader)
        if nil != resolveErr {
            t.Fatalf("accept %q: unexpected error: %v", acceptHeader, resolveErr)
        }

        if "text/html" != normalizeMime(resolved.ContentType()) {
            t.Fatalf("accept %q: expected the lexically first configured serializer, got %s", acceptHeader, resolved.ContentType())
        }
    }
}

/* @info a member whose q parameter falls outside the qvalue grammar is dropped whole: the previous leniency kept the member at full acceptance, so application/json;q=abc outweighed the sibling the client actually weighted */
func TestResolveByAcceptHeader_MalformedQualityDropsTheMember(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
        MimeTextPlain:       NewPlainTextSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    resolved, resolveErr := manager.ResolveByAcceptHeader("application/json;q=abc, text/plain")
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if false == strings.HasPrefix(resolved.ContentType(), MimeTextPlain) {
        t.Fatalf("expected the malformed member to be dropped and text/plain served, got %s", resolved.ContentType())
    }

    _, allDroppedErr := manager.ResolveByAcceptHeader("text/plain;q=NaN")
    if nil == allDroppedErr {
        t.Fatalf("expected a header whose every member is malformed to be answered with an error")
    }

    if true == errors.Is(allDroppedErr, ErrNotAcceptable) {
        t.Fatalf("a malformed q is not a refusal: expected the no-acceptable-mime error, got %v", allDroppedErr)
    }
}

/* @info a comma inside a quoted parameter value stays inside its member: without quote awareness the refusal in text/plain;version="1,2";q=0 detached from the type it covered and the client was served the very representation it refused */
func TestResolveByAcceptHeader_QuotedCommaKeepsTheRefusal(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeTextPlain: NewPlainTextSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    _, resolveErr := manager.ResolveByAcceptHeader(`text/plain;version="1,2";q=0`)
    if false == errors.Is(resolveErr, ErrNotAcceptable) {
        t.Fatalf("expected the quoted refusal to be honoured as not acceptable, got %v", resolveErr)
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
