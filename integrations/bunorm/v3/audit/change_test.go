package audit

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "os/exec"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

func TestAuditFieldName_SkipsManyToManyRelationField(t *testing.T) {
    field := reflect.StructField{
        Name: "Items",
        Type: reflect.TypeOf([]int{}),
        Tag:  `bun:"m2m:order_to_items,join:Order=Item"`,
    }

    _, skip := auditFieldName(field)
    if false == skip {
        t.Fatalf("expected an m2m relation field to be skipped like rel: fields, so a loaded relation is not diffed into the audit row")
    }
}

const redactedValueLiteral = "<redacted>"

type product struct {
    Id    int64  `bun:"id,pk"`
    Name  string `bun:"name"`
    Price int    `bun:"price"`
}

func findChange(changes []Change, field string) (Change, bool) {
    for _, change := range changes {
        if field == change.Field {
            return change, true
        }
    }
    return Change{}, false
}

func TestChangeSet_UpdateCapturesOnlyChangedFields(t *testing.T) {
    before := product{Id: 1, Name: "old", Price: 10}
    after := product{Id: 1, Name: "new", Price: 10}

    changes := ChangeSet(before, after)

    if 1 != len(changes) {
        t.Fatalf("expected exactly one change, got %d (%+v)", len(changes), changes)
    }

    nameChange, found := findChange(changes, "name")
    if false == found {
        t.Fatalf("expected a name change")
    }

    if "old" != nameChange.Old || "new" != nameChange.New {
        t.Fatalf("unexpected name change: %+v", nameChange)
    }
}

/* an empty change-set must always serialise as [], never as the JSON literal null: the delete path already writes [] for the same "nothing recorded" meaning, and a consumer running jsonb_array_length(changes) over the trail errors or reads 1 on null, which is also indistinguishable from a genuinely absent change-set. */
func TestChangeSet_EmptyResultAlwaysSerialisesAsAnEmptyArray(t *testing.T) {
    identical := product{Id: 1, Name: "same", Price: 10}

    cases := map[string][]Change{
        "idempotent update": ChangeSet(identical, identical),
        "no models at all":  ChangeSet(nil, nil),
        "non-struct insert": ChangeSet(nil, "not-a-model"),
        "non-struct delete": ChangeSet(map[string]string{"a": "b"}, nil),
        "typed nil pointer": ChangeSet(nil, (*product)(nil)),
    }

    for name, changes := range cases {
        if 0 != len(changes) {
            t.Fatalf("%s: expected an empty change-set, got %+v", name, changes)
        }

        payload, marshalErr := json.Marshal(changes)
        if nil != marshalErr {
            t.Fatalf("%s: marshal changes: %v", name, marshalErr)
        }

        if "[]" != string(payload) {
            t.Fatalf("%s: expected an empty change-set to serialise as [], got %s", name, payload)
        }
    }
}

func TestChangeSet_InsertHasNewOnly(t *testing.T) {
    changes := ChangeSet(nil, product{Id: 1, Name: "fresh", Price: 5})

    nameChange, found := findChange(changes, "name")
    if false == found || nil != nameChange.Old || "fresh" != nameChange.New {
        t.Fatalf("unexpected insert change: %+v", nameChange)
    }
}

func TestChangeSet_DeleteHasOldOnly(t *testing.T) {
    changes := ChangeSet(product{Id: 1, Name: "gone", Price: 5}, nil)

    nameChange, found := findChange(changes, "name")
    if false == found || "gone" != nameChange.Old || nil != nameChange.New {
        t.Fatalf("unexpected delete change: %+v", nameChange)
    }
}

type account struct {
    Id          int64                                `bun:"id,pk"`
    Email       string                               `bun:"email"`
    ApiKey      string                               `bun:"api_key" audit:"redact"`
    Password    encrypt.EncryptedString              `bun:"password"`
    LookupEmail encrypt.EncryptedDeterministicString `bun:"lookup_email"`
}

type nullableSecretAccount struct {
    Id       int64                                 `bun:"id,pk"`
    Password *encrypt.EncryptedString              `bun:"password"`
    Lookup   *encrypt.EncryptedDeterministicString `bun:"lookup"`
}

func TestChangeSet_RedactsPointerEncryptedFields(t *testing.T) {
    oldPassword := encrypt.EncryptedString("old-secret")
    newPassword := encrypt.EncryptedString("new-secret")
    oldLookup := encrypt.EncryptedDeterministicString("old@example.com")
    newLookup := encrypt.EncryptedDeterministicString("new@example.com")

    before := nullableSecretAccount{Id: 1, Password: &oldPassword, Lookup: &oldLookup}
    after := nullableSecretAccount{Id: 1, Password: &newPassword, Lookup: &newLookup}

    changes := ChangeSet(before, after)

    passwordChange, found := findChange(changes, "password")
    if false == found {
        t.Fatalf("expected the pointer encrypted field to still be recorded as changed")
    }
    if "<redacted>" != passwordChange.Old || "<redacted>" != passwordChange.New {
        t.Fatalf("expected the pointer encrypted field value to be redacted: %+v", passwordChange)
    }

    lookupChange, found := findChange(changes, "lookup")
    if false == found {
        t.Fatalf("expected the pointer deterministic-encrypted field to still be recorded as changed")
    }
    if "<redacted>" != lookupChange.Old || "<redacted>" != lookupChange.New {
        t.Fatalf("expected the pointer deterministic-encrypted field value to be redacted: %+v", lookupChange)
    }

    serialized, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("could not marshal changes: %v", marshalErr)
    }
    for _, secret := range []string{"old-secret", "new-secret", "old@example.com", "new@example.com"} {
        if true == strings.Contains(string(serialized), secret) {
            t.Fatalf("plaintext secret %q leaked into the serialized audit changes: %s", secret, serialized)
        }
    }
}

type EmbeddedAuditFields struct {
    Status    string `bun:"status"`
    UpdatedBy string `bun:"updated_by"`
}

type orderRow struct {
    bun.BaseModel `bun:"table:orders"`
    EmbeddedAuditFields
    Id    int64 `bun:"id,pk"`
    Total int   `bun:"total"`
}

func TestChangeSet_CapturesPromotedEmbeddedStructFields(t *testing.T) {
    before := orderRow{EmbeddedAuditFields: EmbeddedAuditFields{Status: "open", UpdatedBy: "alice"}, Id: 1, Total: 100}
    after := orderRow{EmbeddedAuditFields: EmbeddedAuditFields{Status: "closed", UpdatedBy: "alice"}, Id: 1, Total: 150}

    changes := ChangeSet(before, after)

    statusChange, found := findChange(changes, "status")
    if false == found || "open" != statusChange.Old || "closed" != statusChange.New {
        t.Fatalf("expected the embedded status field to be captured: %+v", changes)
    }

    if _, found := findChange(changes, "total"); false == found {
        t.Fatalf("expected the top-level total field to be captured: %+v", changes)
    }

    if _, found := findChange(changes, "updated_by"); true == found {
        t.Fatalf("did not expect the unchanged embedded updated_by field: %+v", changes)
    }
}

type unexportedEmbed struct {
    Status string `bun:"status"`
    Secret string `audit:"redact" bun:"secret"`
}

type unexportedEmbedRow struct {
    bun.BaseModel `bun:"table:unexported_embed_rows"`
    unexportedEmbed
    Id int64 `bun:"id,pk"`
}

func TestChangeSet_CapturesFieldsPromotedFromUnexportedTypedEmbed(t *testing.T) {
    before := unexportedEmbedRow{unexportedEmbed: unexportedEmbed{Status: "open", Secret: "old-secret"}, Id: 1}
    after := unexportedEmbedRow{unexportedEmbed: unexportedEmbed{Status: "closed", Secret: "new-secret"}, Id: 1}

    changes := ChangeSet(before, after)

    statusChange, found := findChange(changes, "status")
    if false == found || "open" != statusChange.Old || "closed" != statusChange.New {
        t.Fatalf("expected the status field promoted from an unexported-typed embed to be captured: %+v", changes)
    }

    secretChange, found := findChange(changes, "secret")
    if false == found {
        t.Fatalf("expected the redacted secret field promoted from an unexported-typed embed to be captured: %+v", changes)
    }
    if "old-secret" == secretChange.Old || "new-secret" == secretChange.New {
        t.Fatalf("expected the redact-tagged embedded field value to be redacted: %+v", secretChange)
    }
}

func TestChangeSet_RedactsTaggedAndEncryptedFields(t *testing.T) {
    before := account{Id: 1, Email: "a@example.com", ApiKey: "old-key", Password: "old-secret", LookupEmail: "old@example.com"}
    after := account{Id: 1, Email: "b@example.com", ApiKey: "new-key", Password: "new-secret", LookupEmail: "new@example.com"}

    changes := ChangeSet(before, after)

    emailChange, found := findChange(changes, "email")
    if false == found || "a@example.com" != emailChange.Old || "b@example.com" != emailChange.New {
        t.Fatalf("expected the plain email change to pass through: %+v", emailChange)
    }

    apiKeyChange, found := findChange(changes, "api_key")
    if false == found {
        t.Fatalf("expected the tagged field to still be recorded as changed")
    }
    if "old-key" == apiKeyChange.Old || "new-key" == apiKeyChange.New {
        t.Fatalf("expected the tagged field value to be redacted: %+v", apiKeyChange)
    }

    passwordChange, found := findChange(changes, "password")
    if false == found {
        t.Fatalf("expected the encrypted field to still be recorded as changed")
    }
    if "old-secret" == passwordChange.Old || "new-secret" == passwordChange.New {
        t.Fatalf("expected the encrypted field value to be redacted: %+v", passwordChange)
    }

    lookupChange, found := findChange(changes, "lookup_email")
    if false == found {
        t.Fatalf("expected the deterministic-encrypted field to still be recorded as changed")
    }
    if "old@example.com" == lookupChange.Old || "new@example.com" == lookupChange.New {
        t.Fatalf("expected the deterministic-encrypted field value to be redacted: %+v", lookupChange)
    }
}

func TestChangeSet_NestedEncryptedValueRedactedInChangesJson(t *testing.T) {
    type contactDetails struct {
        Email encrypt.EncryptedString `bun:"email"`
        City  string                  `bun:"city"`
    }
    type customer struct {
        Id      int64          `bun:"id,pk"`
        Contact contactDetails `bun:"contact"`
    }

    before := customer{Id: 1, Contact: contactDetails{Email: "old-secret@example.com", City: "Bucharest"}}
    after := customer{Id: 1, Contact: contactDetails{Email: "new-secret@example.com", City: "Bucharest"}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "example.com") {
        t.Fatalf("encrypted plaintext leaked into audit changes json: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredInsideNamedStructField(t *testing.T) {
    type credentials struct {
        Token string `bun:"token" audit:"redact"`
        Label string `bun:"label"`
    }
    type account struct {
        Id    int64       `bun:"id,pk"`
        Creds credentials `bun:"creds"`
    }

    before := account{Id: 1, Creds: credentials{Token: "super-secret-old", Label: "primary"}}
    after := account{Id: 1, Creds: credentials{Token: "super-secret-new", Label: "primary"}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "super-secret") {
        t.Fatalf("redact-tagged plaintext leaked into audit changes json: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredInsideSliceOfStructs(t *testing.T) {
    type lineItem struct {
        Secret string `bun:"secret" audit:"redact"`
        Sku    string `bun:"sku"`
    }
    type order struct {
        Id    int64      `bun:"id,pk"`
        Lines []lineItem `bun:"lines"`
    }

    before := order{Id: 1, Lines: []lineItem{{Secret: "alpha-old", Sku: "A1"}}}
    after := order{Id: 1, Lines: []lineItem{{Secret: "alpha-new", Sku: "A1"}}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "alpha-") {
        t.Fatalf("redact-tagged plaintext leaked into audit changes json from a slice element: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredInsideInterfaceField(t *testing.T) {
    type credentials struct {
        Token string `bun:"token" audit:"redact"`
        Label string `bun:"label"`
    }
    type document struct {
        Id   int64 `bun:"id,pk"`
        Data any   `bun:"data"`
    }

    before := document{Id: 1, Data: credentials{Token: "iface-secret-old", Label: "primary"}}
    after := document{Id: 1, Data: credentials{Token: "iface-secret-new", Label: "primary"}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "iface-secret") {
        t.Fatalf("redact-tagged plaintext leaked into audit changes json through an interface field: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredWhenInterfaceIsNestedInsideStructField(t *testing.T) {
    type credentials struct {
        Token string `bun:"token" audit:"redact"`
        Label string `bun:"label"`
    }
    type wrapper struct {
        Inner any `bun:"inner"`
    }
    type document struct {
        Id   int64   `bun:"id,pk"`
        Wrap wrapper `bun:"wrap"`
    }

    before := document{Id: 1, Wrap: wrapper{Inner: credentials{Token: "deep-secret-old", Label: "primary"}}}
    after := document{Id: 1, Wrap: wrapper{Inner: credentials{Token: "deep-secret-new", Label: "primary"}}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "deep-secret") {
        t.Fatalf("redact-tagged plaintext leaked through an interface nested one level inside a struct field: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredInSecondSiblingOfSameTypeReachedViaInterface(t *testing.T) {
    type credentials struct {
        Token string `bun:"token" audit:"redact"`
        Label string `bun:"label"`
    }
    type holder struct {
        Data any `bun:"data"`
    }
    type pair struct {
        First  holder `bun:"first"`
        Second holder `bun:"second"`
    }
    type document struct {
        Id   int64 `bun:"id,pk"`
        Pair pair  `bun:"pair"`
    }

    before := document{Id: 1, Pair: pair{
        First:  holder{Data: "plain-old"},
        Second: holder{Data: credentials{Token: "sibling-secret-old", Label: "primary"}},
    }}
    after := document{Id: 1, Pair: pair{
        First:  holder{Data: "plain-new"},
        Second: holder{Data: credentials{Token: "sibling-secret-new", Label: "primary"}},
    }}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "sibling-secret") {
        t.Fatalf("redact-tagged plaintext leaked through a same-typed sibling whose redact content was only reachable via an interface field: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactsEncryptedMapKeyPlaintext(t *testing.T) {
    type document struct {
        Id   int64                              `bun:"id,pk"`
        Meta map[encrypt.EncryptedString]string `bun:"meta"`
    }

    before := document{Id: 1, Meta: map[encrypt.EncryptedString]string{encrypt.EncryptedString("key-secret-old"): "primary"}}
    after := document{Id: 1, Meta: map[encrypt.EncryptedString]string{encrypt.EncryptedString("key-secret-new"): "primary"}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "key-secret") {
        t.Fatalf("encrypted map-key plaintext leaked into changes json (json serializes string-kind keys directly, bypassing MarshalJSON): %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

func TestChangeSet_RedactTagHonoredInsideMapOfInterfaceValues(t *testing.T) {
    type credentials struct {
        Token string `bun:"token" audit:"redact"`
        Label string `bun:"label"`
    }
    type document struct {
        Id   int64          `bun:"id,pk"`
        Meta map[string]any `bun:"meta"`
    }

    before := document{Id: 1, Meta: map[string]any{"k": credentials{Token: "map-secret-old", Label: "primary"}}}
    after := document{Id: 1, Meta: map[string]any{"k": credentials{Token: "map-secret-new", Label: "primary"}}}

    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "map-secret") {
        t.Fatalf("redact-tagged plaintext leaked through an interface value inside a map: %s", payload)
    }
    if false == strings.Contains(string(payload), "redacted") {
        t.Fatalf("expected the redacted marker in changes json: %s", payload)
    }
}

type EmbeddedSecret struct {
    Token string `bun:"token"`
}

type accountWithRedactedEmbed struct {
    EmbeddedSecret `audit:"redact"`
    Id             int64 `bun:"id,pk"`
}

func TestChangeSet_RedactTagOnEmbeddedStructRedactsPromotedFields(t *testing.T) {
    before := accountWithRedactedEmbed{EmbeddedSecret: EmbeddedSecret{Token: "secret-old"}, Id: 1}
    after := accountWithRedactedEmbed{EmbeddedSecret: EmbeddedSecret{Token: "secret-new"}, Id: 1}

    changes := ChangeSet(before, after)

    encoded, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        t.Fatalf("marshal changes: %v", marshalErr)
    }

    if true == strings.Contains(string(encoded), "secret-old") || true == strings.Contains(string(encoded), "secret-new") {
        t.Fatalf("an audit:\"redact\" tag on the embedded field must mask the promoted field plaintext, got %s", encoded)
    }

    found := false
    for _, change := range changes {
        if "token" == change.Field {
            found = true
            if redactedValueLiteral != change.Old || redactedValueLiteral != change.New {
                t.Fatalf("promoted token must be redacted, got old=%v new=%v", change.Old, change.New)
            }
        }
    }
    if false == found {
        t.Fatalf("expected a redacted token change, got %s", encoded)
    }
}

type docWithMap struct {
    Id   int64          `bun:"id,pk"`
    Meta map[string]any `bun:"meta"`
}

func TestChangeSet_SelfReferentialMapDoesNotStackOverflow(t *testing.T) {
    cyclic := map[string]any{}
    cyclic["self"] = cyclic

    before := docWithMap{Id: 1}
    after := docWithMap{Id: 1, Meta: cyclic}

    changes := ChangeSet(before, after)
    if 0 == len(changes) {
        t.Fatalf("expected a change for the meta field")
    }
}

type docWithSlice struct {
    Id   int64 `bun:"id,pk"`
    Data []any `bun:"data"`
}

func TestChangeSet_SelfReferentialSliceDoesNotStackOverflow(t *testing.T) {
    cyclic := make([]any, 1)
    cyclic[0] = cyclic

    before := docWithSlice{Id: 1}
    after := docWithSlice{Id: 1, Data: cyclic}

    changes := ChangeSet(before, after)
    if 0 == len(changes) {
        t.Fatalf("expected a change for the data field")
    }
}

func TestValueContainsRedactTag_SharedBackingArraySliceNotFalseDeduped(t *testing.T) {
    type secretHolder struct {
        Secret string `audit:"redact"`
    }
    type carrier struct {
        Head []any
        Full []any
    }

    /* Head and Full share one backing array; Head[:1] is benign and is walked first, recording the backing-array pointer. Full carries the redact-tagged secretHolder at index 1, past Head's length. A pointer-only visit key would dedup Full against Head and skip the secret (leaking it); the pointer+length key must let Full be traversed so the redact tag is detected. */
    backing := []any{"benign", secretHolder{Secret: "leak-me"}}
    value := carrier{Head: backing[:1], Full: backing}

    if false == valueContainsRedactTag(value) {
        t.Fatalf("expected the redact-tagged secretHolder at Full[1] to be detected; the longer slice shares a backing array with the shorter Head[:1] and was falsely deduped, skipping the secret")
    }
}

/* the reproductions below cannot run inside the test binary: a stack overflow is a runtime fatal error that no recover contains, and a spinning element chase never returns at all, so either one takes the whole binary down with it. TestMain therefore doubles as a probe entry point — when the environment variable names a probe the process runs that reproduction and exits instead of running the suite — and each test re-executes this binary, bounds it with a deadline and asserts on the child's exit status. */
const changeProbeEnvironmentVariable = "MELODY_AUDIT_CHANGE_PROBE"

func TestMain(mainInstance *testing.M) {
    probeName := os.Getenv(changeProbeEnvironmentVariable)
    if "" != probeName {
        runChangeProbe(probeName)

        os.Exit(0)
    }

    os.Exit(mainInstance.Run())
}

/* type cyclicAttributes is the map shape the type walk cannot answer by descending: its element is itself */
type cyclicAttributes map[string]cyclicAttributes

type cyclicMapModel struct {
    Id         int64            `bun:"id,pk"`
    Attributes cyclicAttributes `bun:"attributes"`
}

/* type cyclicSlice is the slice shape whose element chase never reaches a non-slice */
type cyclicSlice []cyclicSlice

type cyclicSliceModel struct {
    Id   int64       `bun:"id,pk"`
    Data cyclicSlice `bun:"data"`
}

/* type cyclicPointer is the pointer shape whose element chase never reaches a non-pointer */
type cyclicPointer *cyclicPointer

type cyclicPointerModel struct {
    Id     int64         `bun:"id,pk"`
    Cursor cyclicPointer `bun:"cursor"`
}

type cyclicPointerCarrier struct {
    Cursor cyclicPointer
}

type cyclicPointerNestedModel struct {
    Id      int64                `bun:"id,pk"`
    Carrier cyclicPointerCarrier `bun:"carrier"`
}

/* CyclicEmbeddedNode is the embed Go permits only through a pointer, and the pointer may close a loop with no nil to end it */
type CyclicEmbeddedNode struct {
    *CyclicEmbeddedNode
    Name string `bun:"name"`
}

func runChangeProbe(probeName string) {
    switch probeName {
    case "cyclicMapType":
        ChangeSet(cyclicMapModel{Id: 1}, cyclicMapModel{Id: 2})

    case "cyclicSliceType":
        ChangeSet(cyclicSliceModel{Id: 1}, cyclicSliceModel{Id: 2})

    case "cyclicPointerType":
        ChangeSet(cyclicPointerModel{Id: 1}, cyclicPointerModel{Id: 2})

    case "cyclicPointerNestedType":
        ChangeSet(cyclicPointerNestedModel{Id: 1}, cyclicPointerNestedModel{Id: 2})

    case "cyclicEmbeddedPointer":
        before := &CyclicEmbeddedNode{Name: "before"}
        before.CyclicEmbeddedNode = before

        after := &CyclicEmbeddedNode{Name: "after"}
        after.CyclicEmbeddedNode = after

        ChangeSet(before, after)

    case "mutuallyCyclicEmbeddedPointer":
        before := &CyclicEmbeddedNode{Name: "before"}
        beforeNext := &CyclicEmbeddedNode{Name: "before-next"}
        before.CyclicEmbeddedNode = beforeNext
        beforeNext.CyclicEmbeddedNode = before

        after := &CyclicEmbeddedNode{Name: "after"}
        afterNext := &CyclicEmbeddedNode{Name: "after-next"}
        after.CyclicEmbeddedNode = afterNext
        afterNext.CyclicEmbeddedNode = after

        ChangeSet(before, after)

    default:
        os.Exit(97)
    }
}

func assertChangeProbeExitsCleanly(t *testing.T, probeName string, budget time.Duration) {
    t.Helper()

    binaryPath, executableErr := os.Executable()
    if nil != executableErr {
        t.Fatalf("could not locate the test binary to re-execute: %v", executableErr)
    }

    ctx, cancel := context.WithTimeout(context.Background(), budget)
    defer cancel()

    command := exec.CommandContext(ctx, binaryPath)
    command.Env = append(os.Environ(), changeProbeEnvironmentVariable+"="+probeName)

    combinedOutput, runErr := command.CombinedOutput()

    if nil != ctx.Err() {
        t.Fatalf("the %s probe was still running after %s and had to be killed, so the walk does not terminate; output: %s", probeName, budget, combinedOutput)
    }

    if nil == runErr {
        return
    }

    exitErr := (*exec.ExitError)(nil)
    if true == errors.As(runErr, &exitErr) {
        t.Fatalf("the %s probe died with exit status %d instead of returning; output: %s", probeName, exitErr.ExitCode(), combinedOutput)
    }

    t.Fatalf("could not run the %s probe: %v; output: %s", probeName, runErr, combinedOutput)
}

/* a field whose map type contains itself must not send the redact-tag type walk into unbounded recursion; the walk runs for every non-anonymous field of every audited insert, update and delete, and a stack overflow there is a fatal error that takes the process down with no recover and no rollback */
func TestIsRedactedField_SelfReferentialMapTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicMapType", 30*time.Second)
}

/* `type Node []Node` is legal Go and its element chase never reaches a non-slice, so the chase spins at full processor instead of overflowing: the process never dies, it just stops serving */
func TestIsRedactedField_SelfReferentialSliceTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicSliceType", 30*time.Second)
}

/* `type Pointer *Pointer` is legal Go and spins the same chase, both where the field type is dereferenced for the encrypted string check and inside the redact-tag type walk */
func TestIsRedactedField_SelfReferentialPointerTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicPointerType", 30*time.Second)
}

/* the same pointer shape reached as a nested struct field, where the value walk dereferences sub-field types of its own */
func TestValueContainsRedactTag_SelfReferentialPointerSubFieldTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicPointerNestedType", 30*time.Second)
}

/* Go rejects `type Node struct { Node }` but permits `type Node struct { *Node }`, so an embed can point back at the struct the walk is already inside; the embed recursion has no nil to stop on and overflows the stack, which a deferred recover around ChangeSet does not catch */
func TestChangeSet_SelfEmbeddedPointerCycleTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicEmbeddedPointer", 30*time.Second)
}

/* the same loop closed over two nodes rather than one */
func TestChangeSet_MutuallyEmbeddedPointerCycleTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "mutuallyCyclicEmbeddedPointer", 30*time.Second)
}

/* the cycle guard must not swallow a finite embed chain: three distinct nodes are three distinct pointers, and every field they carry still belongs in the change-set */
func TestChangeSet_FiniteEmbeddedPointerChainIsStillWalked(t *testing.T) {
    before := &CyclicEmbeddedNode{Name: "root"}
    before.CyclicEmbeddedNode = &CyclicEmbeddedNode{Name: "middle"}
    before.CyclicEmbeddedNode.CyclicEmbeddedNode = &CyclicEmbeddedNode{Name: "leaf-before"}

    after := &CyclicEmbeddedNode{Name: "root"}
    after.CyclicEmbeddedNode = &CyclicEmbeddedNode{Name: "middle"}
    after.CyclicEmbeddedNode.CyclicEmbeddedNode = &CyclicEmbeddedNode{Name: "leaf-after"}

    changes := ChangeSet(before, after)

    nameChange, found := findChange(changes, "name")
    if false == found {
        t.Fatalf("expected the deepest embed of a finite chain to still be diffed, got %+v", changes)
    }
    if "leaf-before" != nameChange.Old || "leaf-after" != nameChange.New {
        t.Fatalf("expected the change from the third node of the chain, got %+v", nameChange)
    }
}

/* the redact-tag type walk must keep answering true for a self-referential type that does carry a tag somewhere; guarding the cycle may not turn into refusing to look */
func TestIsRedactedField_CyclicTypeStillReportsAReachableRedactTag(t *testing.T) {
    type taggedNode struct {
        Secret   string `audit:"redact"`
        Children []any
    }
    type cyclicHolder struct {
        Attributes cyclicAttributes
        Tagged     taggedNode
    }

    holderType := reflect.TypeOf(cyclicHolder{})

    if true == isRedactedField(holderType.Field(0)) {
        t.Fatalf("a cyclic map type that reaches no redact tag must not be reported as redacted, or every self-referential shape is replaced by a placeholder")
    }
    if false == isRedactedField(holderType.Field(1)) {
        t.Fatalf("a redact tag reachable from the field type must still be found once the cycle guard is in place")
    }
}

type changeTestCompartmentRef struct{}

func (instance changeTestCompartmentRef) CipherName() string {
    return "change-test-compartment"
}

/* the compartment-bound generic forms instantiate a distinct reflect.Type per marker, so the identity list the redaction used to match could never enumerate them: an Iban typed EncryptedStringFor[ref] reached the change-set as live plaintext */
func TestChangeSet_RedactsTheCompartmentBoundEncryptedTypes(t *testing.T) {
    type compartmentAccount struct {
        Id            int64                                                     `bun:"id,pk"`
        Iban          encrypt.EncryptedStringFor[changeTestCompartmentRef]      `bun:"iban"`
        SearchableTin encrypt.EncryptedDeterministicStringFor[changeTestCompartmentRef] `bun:"tin"`
    }

    before := compartmentAccount{Id: 1, Iban: "DE89370400440532013000", SearchableTin: "123"}
    after := compartmentAccount{Id: 1, Iban: "FR7630006000011234567890189", SearchableTin: "456"}

    changes := ChangeSet(before, after)
    if 2 != len(changes) {
        t.Fatalf("expected both encrypted fields to be recorded as changed, got %d: %v", len(changes), changes)
    }

    for _, change := range changes {
        if "<redacted>" != change.Old || "<redacted>" != change.New {
            t.Fatalf("expected the compartment-bound field %q to be redacted, got old=%v new=%v", change.Field, change.Old, change.New)
        }
    }
}

/* a struct field whose TYPE holds an encrypted column one level down used to read as tag-free through the type walk while the value walk disagreed; both walks must answer redacted */
func TestChangeSet_RedactsAStructWhoseTypeNestsAnEncryptedColumn(t *testing.T) {
    type paymentDetails struct {
        Iban encrypt.EncryptedString
    }

    type customer struct {
        Id      int64          `bun:"id,pk"`
        Payment paymentDetails `bun:"payment"`
    }

    before := customer{Id: 1, Payment: paymentDetails{Iban: "DE89"}}
    after := customer{Id: 1, Payment: paymentDetails{Iban: "FR76"}}

    changes := ChangeSet(before, after)
    if 1 != len(changes) {
        t.Fatalf("expected one change, got %d: %v", len(changes), changes)
    }

    if "<redacted>" != changes[0].Old || "<redacted>" != changes[0].New {
        t.Fatalf("expected the nested encrypted column to redact the whole field, got old=%v new=%v", changes[0].Old, changes[0].New)
    }
}

/* under plain omitempty the transition active true→false marshalled byte-identical to a delete's before-image; the present zero must render */
func TestChange_MarshalsAPresentZeroValue(t *testing.T) {
    type flagRow struct {
        Id     int64 `bun:"id,pk"`
        Active bool  `bun:"active"`
    }

    updateChanges := ChangeSet(flagRow{Id: 1, Active: true}, flagRow{Id: 1, Active: false})

    updatePayload, updateErr := json.Marshal(updateChanges)
    if nil != updateErr {
        t.Fatalf("marshal update: %v", updateErr)
    }

    if false == strings.Contains(string(updatePayload), `"new":false`) {
        t.Fatalf("expected the present zero to render, got %s", updatePayload)
    }

    deleteChanges := changeSetWithIgnore(flagRow{Id: 1, Active: true}, nil, nil)

    deletePayload, deleteErr := json.Marshal(deleteChanges)
    if nil != deleteErr {
        t.Fatalf("marshal delete: %v", deleteErr)
    }

    if true == strings.Contains(string(deletePayload), `"new"`) {
        t.Fatalf("expected the absent side to stay out of a delete's before-image, got %s", deletePayload)
    }

    if false == strings.Contains(string(deletePayload), `"old":true`) {
        t.Fatalf("expected the delete's before-image to keep its old side, got %s", deletePayload)
    }

    /* the boxed false above is non-nil either way, so it cannot separate presence from the plain nil check; the value that can is a PRESENT nil — an any-typed field emptied to nil must render "new":null, where absence would erase the transition */
    type attributeRow struct {
        Id    int64 `bun:"id,pk"`
        Extra any   `bun:"extra"`
    }

    nilChanges := ChangeSet(attributeRow{Id: 1, Extra: "x"}, attributeRow{Id: 1, Extra: nil})

    nilPayload, nilErr := json.Marshal(nilChanges)
    if nil != nilErr {
        t.Fatalf("marshal nil transition: %v", nilErr)
    }

    if false == strings.Contains(string(nilPayload), `"new":null`) {
        t.Fatalf("expected the present nil to render as null, got %s", nilPayload)
    }
}

/* the one shape where the TYPE walk answers alone: an empty slice against a nil slice records a change whose values hold no element for the value walk to inspect, so only the field type can say the element carries an encrypted column — under the pre-repair type walk this change-set carried the (empty) containers unredacted while every populated shape was saved by the value walk */
func TestChangeSet_RedactsAnEmptyContainerOfAnEncryptedCarryingType(t *testing.T) {
    type paymentDetails struct {
        Iban encrypt.EncryptedString
    }

    type customer struct {
        Id       int64            `bun:"id,pk"`
        Payments []paymentDetails `bun:"payments"`
    }

    before := customer{Id: 1, Payments: []paymentDetails{}}
    after := customer{Id: 1, Payments: nil}

    changes := ChangeSet(before, after)
    if 1 != len(changes) {
        t.Fatalf("expected the container transition to be recorded, got %d: %v", len(changes), changes)
    }

    if "<redacted>" != changes[0].Old {
        t.Fatalf("expected the encrypted-carrying container to be redacted by its type alone, got old=%v", changes[0].Old)
    }
}
