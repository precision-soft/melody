package serializer

import (
    "errors"
    "strconv"
    "strings"
    "testing"

    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
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

/* each available type takes the quality of the most specific range covering it, so an exact range wins over a wildcard regardless of header order, and a q of 0 refuses rather than being ignored */
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

/* a typed nil passes the plain nil comparison, would be stored as a live serializer and would dereference its nil receiver on the first request the negotiation routes to it — the constructor refuses it with the same error the untyped nil gets */
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

/* two spellings collapsing into one normalized mime are refused at construction: map iteration order would decide the surviving serializer, so the winner would change from one boot to the next with the loser dropped silently */
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

/* an empty accept header means the client takes anything, and a header matching nothing available still receives the default representation: a manager deliberately configured without json serves its first configured serializer in lexical mime order instead of refusing every such request while a serializer sits configured beside it */
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

/* a member whose q parameter falls outside the qvalue grammar is dropped whole, so application/json;q=abc never outweighs the sibling the client actually weighted */
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

/* a comma inside a quoted parameter value stays inside its member, so the refusal in text/plain;version="1,2";q=0 stays with the type it covers and that representation is never served */
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

/* a header that simply matches nothing available is not a refusal: the default representation is still served, which is what every client sending a narrow accept header against this framework relies on */
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

/* a refusal that leaves another registered type merely unmatched is a preference, not a refusal of the whole manager, so Accept: application/json;q=0 against a manager holding plain text beside json is served plain text rather than 406. The refused type still may not be served, by the negotiation or by the fallback, so the json-first default steps aside for it. */
func TestResolveByAcceptHeader_ARefusedTypeLeavesTheUnrefusedOneServable(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
        MimeTextPlain:       NewPlainTextSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    /* the second spelling refuses json through a wildcard rather than by name, so the answer cannot depend on the refusal being exact */
    for _, acceptHeader := range []string{"application/json;q=0", "application/*;q=0"} {
        resolved, resolveErr := manager.ResolveByAcceptHeader(acceptHeader)
        if nil != resolveErr {
            t.Fatalf("accept %q: expected the unrefused type to be served, got %v", acceptHeader, resolveErr)
        }

        if false == strings.HasPrefix(resolved.ContentType(), MimeTextPlain) {
            t.Fatalf("accept %q: expected %s, got %s", acceptHeader, MimeTextPlain, resolved.ContentType())
        }
    }

    /* the refusal of every registered type stays a refusal: without this half the not-acceptable branch could simply be deleted */
    _, everyTypeRefusedErr := manager.ResolveByAcceptHeader("*/*;q=0")
    if false == errors.Is(everyTypeRefusedErr, ErrNotAcceptable) {
        t.Fatalf("expected a header refusing every registered type to stay not acceptable, got %v", everyTypeRefusedErr)
    }
}

/* Get answers false for a mime that normalizes away to nothing, before it ever touches the map. An empty header value, a whitespace-only one and a bare parameter list all arrive here from a caller reading a Content-Type off a request that carried none, and without the guard the lookup would run with the empty key, the key the constructor refuses to register. */
func TestSerializerManager_Get_RefusesAMimeThatNormalizesToNothing(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    for _, emptyMime := range []string{"", "   ", "\t", ";charset=utf-8", "  ; charset=utf-8"} {
        resolved, exists := manager.Get(emptyMime)
        if true == exists || nil != resolved {
            t.Fatalf("expected %q to be refused as a mime, got %#v", emptyMime, resolved)
        }
    }
}

/* a mime the manager simply does not carry is a miss too, and it has to be told apart from the one above: both answer false, so a guard that returned early for every input would keep a test that only asserts the miss green while the registered serializer became unreachable. */
func TestSerializerManager_Get_AnswersTheRegisteredSerializerAndMissesTheOthers(t *testing.T) {
    registered := NewJsonSerializer()

    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: registered,
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    resolved, exists := manager.Get("Application/JSON; charset=utf-8")
    if false == exists || registered != resolved {
        t.Fatalf("expected the registered serializer for a spelled-out mime, got %#v, %t", resolved, exists)
    }

    resolved, exists = manager.Get(MimeTextPlain)
    if true == exists || nil != resolved {
        t.Fatalf("expected an unregistered mime to miss, got %#v, %t", resolved, exists)
    }
}

/* the empty manager is the misconfiguration a deployment produces when the serializer map comes from configuration that resolved to nothing. Both error paths belong to it, and they are NOT the same answer: an empty accept header means "anything", so its failure says no default is configured, while a header that named something says nothing was found for that header, and only the second one carries the header in its context, which is the whole diagnostic. No other test reaches either branch. */
func TestResolveByAcceptHeader_AnEmptyManagerRefusesBothWays(t *testing.T) {
    manager, managerErr := NewSerializerManager(nil)
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    resolved, resolveErr := manager.ResolveByAcceptHeader("")
    if nil == resolveErr || nil != resolved {
        t.Fatalf("expected an empty manager to refuse an empty accept header, got %#v, %v", resolved, resolveErr)
    }

    if "no default serializer configured" != resolveErr.Error() {
        t.Fatalf("unexpected refusal for the empty header: %q", resolveErr.Error())
    }

    resolved, resolveErr = manager.ResolveByAcceptHeader("application/json")
    if nil == resolveErr || nil != resolved {
        t.Fatalf("expected an empty manager to refuse a named accept header, got %#v, %v", resolved, resolveErr)
    }

    if "no serializer found for accept header" != resolveErr.Error() {
        t.Fatalf("unexpected refusal for the named header: %q", resolveErr.Error())
    }

    contextualErr, isContextual := resolveErr.(interface {
        error
        Context() exceptioncontract.Context
    })
    if false == isContextual {
        t.Fatalf("expected the refusal to carry a context, got %T", resolveErr)
    }

    if "application/json" != contextualErr.Context()["accept"] {
        t.Fatalf("expected the refusal to name the header it could not satisfy, got %v", contextualErr.Context()["accept"])
    }
}

/* an empty manager cannot even be asked for a default: defaultSerializer's zero-length branch is what turns "json is absent, take the first configured one" into an honest miss instead of an index into an empty slice. */
func TestSerializerManager_DefaultSerializer_AnEmptyManagerHasNoDefault(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{})
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    resolved, exists := manager.defaultSerializer()
    if true == exists || nil != resolved {
        t.Fatalf("expected an empty manager to have no default, got %#v, %t", resolved, exists)
    }
}

/* a nil map is the same manager as an empty one — the constructor substitutes an empty map rather than carrying the nil into every later lookup, where a read would work and the collision bookkeeping would not. */
func TestNewSerializerManager_ANilMapBuildsAnEmptyManager(t *testing.T) {
    manager, managerErr := NewSerializerManager(nil)
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    if nil == manager.serializersByMime {
        t.Fatalf("expected the nil map to be substituted rather than carried")
    }

    if 0 != len(manager.serializersByMime) {
        t.Fatalf("expected an empty manager, got %d entries", len(manager.serializersByMime))
    }
}

/* normalizeMime is used as an invariant by the matching above it — matchWildcardSubtype normalizes BOTH of its arguments — so a normalization that changed an already-normalized value would make a range match on the first pass and miss on the second. */
func TestNormalizeMime_IsIdempotent(t *testing.T) {
    for _, rawMime := range []string{
        "Application/JSON; charset=utf-8",
        "  TEXT/plain ",
        "application/json",
        "*/*",
        "text/*",
    } {
        once := normalizeMime(rawMime)
        twice := normalizeMime(once)

        if once != twice {
            t.Fatalf("expected normalizeMime to be idempotent on %q, got %q then %q", rawMime, once, twice)
        }
    }
}

func TestResolveByAcceptHeader_CatchAllHonoursTheJsonFirstDefault(t *testing.T) {
    jsonSerializer := NewJsonSerializer()
    halSerializer := NewJsonSerializer()

    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        "application/json":     jsonSerializer,
        "application/hal+json": halSerializer,
    })
    if nil != managerErr {
        t.Fatalf("manager construction failed: %v", managerErr)
    }

    emptyResolved, emptyErr := manager.ResolveByAcceptHeader("")
    if nil != emptyErr {
        t.Fatalf("empty header resolution failed: %v", emptyErr)
    }
    if serializercontract.Serializer(jsonSerializer) != emptyResolved {
        t.Fatalf("expected the empty header to resolve json-first")
    }

    wildcardResolved, wildcardErr := manager.ResolveByAcceptHeader("*/*")
    if nil != wildcardErr {
        t.Fatalf("wildcard resolution failed: %v", wildcardErr)
    }
    if serializercontract.Serializer(jsonSerializer) != wildcardResolved {
        t.Fatalf("expected the catch-all range to resolve json-first the way the empty header does")
    }
}

func TestResolveByAcceptHeader_AJsonTieWithoutJsonStaysLexicallyFirst(t *testing.T) {
    halSerializer := NewJsonSerializer()
    xmlSerializer := NewJsonSerializer()

    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        "application/hal+json": halSerializer,
        "application/xml":      xmlSerializer,
    })
    if nil != managerErr {
        t.Fatalf("manager construction failed: %v", managerErr)
    }

    resolved, resolveErr := manager.ResolveByAcceptHeader("*/*")
    if nil != resolveErr {
        t.Fatalf("wildcard resolution failed: %v", resolveErr)
    }
    if serializercontract.Serializer(halSerializer) != resolved {
        t.Fatalf("expected a tie without json to keep the lexically first candidate")
    }
}

/* acceptListWithTailPast builds an Accept header of one head range, filler ranges and one tail range; with more members than the split cap the tail is what the cap cuts off. */
func acceptListWithTailPast(head string, fillers int, tail string) string {
    members := []string{head}
    for index := 0; index < fillers; index++ {
        members = append(members, "x/"+strconv.Itoa(index))
    }

    return strings.Join(append(members, tail), ", ")
}

/* A header the member cap cut is read as unparsable and refused as not acceptable, since the refusal past the cap (application/json;q=0) is lost with the tail and the wildcard before it would serve json to a client that refused it. The sister list one member short of the cap still honours the refusal and falls to the other type. */
func TestResolveByAcceptHeader_AHeaderCutAtTheCapIsRefused(t *testing.T) {
    manager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: NewJsonSerializer(),
        MimeTextPlain:       NewPlainTextSerializer(),
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    _, resolveErr := manager.ResolveByAcceptHeader(acceptListWithTailPast("*/*;q=1", 63, "application/json;q=0"))
    if false == errors.Is(resolveErr, ErrNotAcceptable) {
        t.Fatalf("expected a header cut at the member cap to be refused, got %v", resolveErr)
    }

    within, resolveErr := manager.ResolveByAcceptHeader(acceptListWithTailPast("*/*;q=1", 62, "application/json;q=0"))
    if nil != resolveErr || false == strings.HasPrefix(within.ContentType(), MimeTextPlain) {
        t.Fatalf("expected the refusal within the cap to fall to the plain serializer, got %v / %v", within, resolveErr)
    }
}
