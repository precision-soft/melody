package audit

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

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

/** @info redact tag on embedded struct */

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
