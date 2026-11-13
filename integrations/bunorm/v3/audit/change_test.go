package audit

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "os/exec"
    "reflect"
    "strconv"
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

func TestChangeSet_LeavesAScanOnlyFieldOut(t *testing.T) {
    type bareOption struct {
        Id    int64 `bun:"id,pk"`
        Name  string `bun:"name"`
        Total int64 `bun:",scanonly"`
    }

    type namedOption struct {
        Id    int64 `bun:"id,pk"`
        Name  string `bun:"name"`
        Total int64 `bun:"total,scanonly"`
    }

    type optionAfterAnother struct {
        Id    int64 `bun:"id,pk"`
        Name  string `bun:"name"`
        Total int64 `bun:"total,nullzero,scanonly"`
    }

    type optionBeforeAnother struct {
        Id    int64 `bun:"id,pk"`
        Name  string `bun:"name"`
        Total int64 `bun:"total,scanonly,nullzero"`
    }

    for _, row := range []struct {
        label  string
        before any
        after  any
    }{
        {label: "bare option", before: bareOption{Id: 1, Name: "old"}, after: bareOption{Id: 1, Name: "new", Total: 7}},
        {label: "option after a column name", before: namedOption{Id: 1, Name: "old"}, after: namedOption{Id: 1, Name: "new", Total: 7}},
        {label: "option after another option", before: optionAfterAnother{Id: 1, Name: "old"}, after: optionAfterAnother{Id: 1, Name: "new", Total: 7}},
        {label: "option before another option", before: optionBeforeAnother{Id: 1, Name: "old"}, after: optionBeforeAnother{Id: 1, Name: "new", Total: 7}},
    } {
        changes := ChangeSet(row.before, row.after)

        if _, found := findChange(changes, "Total"); true == found {
            t.Fatalf("%s: expected the scan-only field left out, got %+v", row.label, changes)
        }

        if _, found := findChange(changes, "total"); true == found {
            t.Fatalf("%s: expected the scan-only field left out, got %+v", row.label, changes)
        }

        if _, found := findChange(changes, "name"); false == found {
            t.Fatalf("%s: expected the column change kept, got %+v", row.label, changes)
        }
    }
}

type shadowedAuditFields struct {
    Name string `bun:"name"`
    Note string `bun:"note"`
}

type shadowingRow struct {
    bun.BaseModel `bun:"table:shadowing_rows"`
    shadowedAuditFields
    Id   int64  `bun:"id,pk"`
    Name string `bun:"name"`
}

type deeplyShadowedAuditFields struct {
    shadowedAuditFields
}

type deeplyShadowingRow struct {
    bun.BaseModel `bun:"table:deeply_shadowing_rows"`
    deeplyShadowedAuditFields
    Id   int64  `bun:"id,pk"`
    Name string `bun:"name"`
}

func TestChangeSet_LeavesOutAnEmbeddedFieldAColumnNearerTheRootShadows(t *testing.T) {
    for _, row := range []struct {
        label  string
        before any
        after  any
    }{
        {
            label:  "one level down",
            before: shadowingRow{Id: 1, Name: "top"},
            after:  shadowingRow{Id: 1, Name: "top", shadowedAuditFields: shadowedAuditFields{Name: "inner", Note: "kept"}},
        },
        {
            label:  "two levels down",
            before: deeplyShadowingRow{Id: 1, Name: "top"},
            after:  deeplyShadowingRow{Id: 1, Name: "top", deeplyShadowedAuditFields: deeplyShadowedAuditFields{shadowedAuditFields{Name: "inner", Note: "kept"}}},
        },
    } {
        changes := ChangeSet(row.before, row.after)

        if _, found := findChange(changes, "name"); true == found {
            t.Fatalf("%s: expected the shadowed embedded field left out, got %+v", row.label, changes)
        }

        if noteChange, found := findChange(changes, "note"); false == found || "kept" != noteChange.New {
            t.Fatalf("%s: expected the embedded field no column shadows kept, got %+v", row.label, changes)
        }
    }

    topChanges := ChangeSet(shadowingRow{Id: 1, Name: "old"}, shadowingRow{Id: 1, Name: "new"})
    if topChange, found := findChange(topChanges, "name"); false == found || "new" != topChange.New {
        t.Fatalf("expected the shadowing column itself recorded, got %+v", topChanges)
    }
}

type optionFirstMeta struct {
    Meta map[string]any `bun:"type:jsonb"`
}

type optionFirstRow struct {
    bun.BaseModel `bun:"table:option_first_rows"`
    optionFirstMeta
    Id   int64          `bun:"id,pk"`
    Data map[string]any `bun:"type:jsonb"`
}

type taggedEmail struct {
    Mail        string `bun:"email"`
    UpdatedBy   string `bun:"updated_by"`
    PhoneNumber string
}

type untaggedEmailRow struct {
    bun.BaseModel `bun:"table:untagged_email_rows"`
    taggedEmail
    Id        int64 `bun:"id,pk"`
    Email     string
    UpdatedBy string
    Phone     string `bun:",column:phone_number"`
}

type untaggedNote struct {
    Note string
}

type otherUntaggedNote struct {
    Note string
}

type taggedNote struct {
    Note string `bun:"note"`
}

type siblingUntaggedRow struct {
    bun.BaseModel `bun:"table:sibling_untagged_rows"`
    untaggedNote
    otherUntaggedNote
    Id int64 `bun:"id,pk"`
}

type siblingTaggedRow struct {
    bun.BaseModel `bun:"table:sibling_tagged_rows"`
    untaggedNote
    *taggedNote
    Id int64 `bun:"id,pk"`
}

type scanOnlyTotal struct {
    Total  int    `bun:"total"`
    Hidden string `bun:"Hidden"`
}

type scanOnlyRootRow struct {
    bun.BaseModel `bun:"table:scan_only_root_rows"`
    scanOnlyTotal
    Id    int64 `bun:"id,pk"`
    Total  int    `bun:"total,scanonly"`
    Hidden string `bun:"-"`
}

type prefixedAddress struct {
    City string `bun:"city"`
}

type prefixedCity struct {
    AddressCity string `bun:"address_city"`
}

type prefixedRow struct {
    bun.BaseModel `bun:"table:prefixed_rows"`
    prefixedCity
    Id      int64           `bun:"id,pk"`
    Address prefixedAddress `bun:"embed:address_"`
}

type nestedPrefixedAddress struct {
    Address prefixedAddress `bun:"embed:address_"`
}

type nestedPrefixedRow struct {
    bun.BaseModel `bun:"table:nested_prefixed_rows"`
    nestedPrefixedAddress
    Id int64 `bun:"id,pk"`
}

type anonymousPrefixedRow struct {
    bun.BaseModel `bun:"table:anonymous_prefixed_rows"`
    prefixedAddress `bun:"embed:address_"`
    Id   int64  `bun:"id,pk"`
    City string `bun:"city"`
}

func TestEmbeddedRowFieldPaths_KeepTheEmbeddedFieldsBunKeeps(t *testing.T) {
    database := newTestDatabase()

    for _, model := range []any{
        optionFirstRow{},
        untaggedEmailRow{},
        siblingUntaggedRow{},
        siblingTaggedRow{},
        scanOnlyRootRow{},
        prefixedRow{},
        anonymousPrefixedRow{},
        shadowingRow{},
        deeplyShadowingRow{},
    } {
        modelType := reflect.TypeOf(model)

        expected := map[string]struct{}{}
        for _, field := range database.Table(modelType).FieldMap {
            if 1 < len(field.Index) && true == modelType.Field(field.Index[0]).Anonymous {
                expected[indexPathKey(field.Index)] = struct{}{}
            }
        }

        actual := map[string]struct{}{}
        for path := range embeddedRowFieldPaths(modelType) {
            first, _, _ := strings.Cut(path, ".")
            index, _ := strconv.Atoi(first)
            if true == modelType.Field(index).Anonymous {
                actual[path] = struct{}{}
            }
        }

        if false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("%s: expected the embedded fields bun keeps %v, got %v", modelType.Name(), expected, actual)
        }
    }
}

func TestChangeSet_RecordsAnEmbeddedFieldBunKeepsAndLeavesOutOneItDrops(t *testing.T) {
    optionFirstChanges := ChangeSet(
        optionFirstRow{Id: 1, Data: map[string]any{"a": 1}},
        optionFirstRow{Id: 1, Data: map[string]any{"a": 1}, optionFirstMeta: optionFirstMeta{Meta: map[string]any{"b": 2}}},
    )
    if 1 != len(optionFirstChanges) {
        t.Fatalf("expected the embedded column bun writes under its own name recorded, got %+v", optionFirstChanges)
    }

    emailChanges := ChangeSet(untaggedEmailRow{Id: 1}, untaggedEmailRow{Id: 1, taggedEmail: taggedEmail{Mail: "inner@example.com", PhoneNumber: "0700"}})
    if 0 != len(emailChanges) {
        t.Fatalf("expected the embedded field an untagged root column shadows left out, got %+v", emailChanges)
    }

    siblingChanges := ChangeSet(
        siblingTaggedRow{Id: 1, taggedNote: &taggedNote{}},
        siblingTaggedRow{Id: 1, untaggedNote: untaggedNote{Note: "untagged"}, taggedNote: &taggedNote{Note: "tagged"}},
    )
    if 1 != len(siblingChanges) || "tagged" != siblingChanges[0].New {
        t.Fatalf("expected only the tagged sibling recorded, got %+v", siblingChanges)
    }
}

func TestChangeSet_RecordsAnEmbedPrefixedStructInsideAnAnonymousEmbed(t *testing.T) {
    changes := ChangeSet(
        nestedPrefixedRow{Id: 1, nestedPrefixedAddress: nestedPrefixedAddress{Address: prefixedAddress{City: "old"}}},
        nestedPrefixedRow{Id: 1, nestedPrefixedAddress: nestedPrefixedAddress{Address: prefixedAddress{City: "new"}}},
    )

    if _, found := findChange(changes, "embed:address_"); false == found {
        t.Fatalf("expected the prefixed struct inside an anonymous embed recorded, got %+v", changes)
    }
}

func TestAuditFieldName_SkipsARelationWhoseOptionFollowsTheName(t *testing.T) {
    for _, tag := range []reflect.StructTag{
        `bun:"author,rel:belongs-to,join:author_id=id"`,
        `bun:"items,m2m:order_to_items,join:Order=Item"`,
        `bun:"rel:has-one,join:id=owner_id"`,
    } {
        if _, skip := auditFieldName(reflect.StructField{Name: "Author", Type: reflect.TypeOf(struct{}{}), Tag: tag}); false == skip {
            t.Fatalf("expected %s skipped as a relation", tag)
        }
    }

    if name, skip := auditFieldName(reflect.StructField{Name: "Meta", Type: reflect.TypeOf(""), Tag: `bun:"type:jsonb"`}); true == skip || "type:jsonb" != name {
        t.Fatalf("expected an option-first column kept under its existing name, got %q %v", name, skip)
    }
}

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

    /* Full shares Head's backing array and holds the redacted element past Head's length */
    backing := []any{"benign", secretHolder{Secret: "leak-me"}}
    value := carrier{Head: backing[:1], Full: backing}

    if false == valueContainsRedactTag(value) {
        t.Fatalf("expected the redact-tagged secretHolder at Full[1] to be detected; the longer slice shares a backing array with the shorter Head[:1] and was falsely deduped, skipping the secret")
    }
}

/* the probes run in a child process, since a stack overflow or a spinning chase takes the whole binary down */
const changeProbeEnvironmentVariable = "MELODY_AUDIT_CHANGE_PROBE"

func TestMain(mainInstance *testing.M) {
    probeName := os.Getenv(changeProbeEnvironmentVariable)
    if "" != probeName {
        runChangeProbe(probeName)

        os.Exit(0)
    }

    os.Exit(mainInstance.Run())
}

type cyclicAttributes map[string]cyclicAttributes

type cyclicMapModel struct {
    Id         int64            `bun:"id,pk"`
    Attributes cyclicAttributes `bun:"attributes"`
}

type cyclicSlice []cyclicSlice

type cyclicSliceModel struct {
    Id   int64       `bun:"id,pk"`
    Data cyclicSlice `bun:"data"`
}

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

func TestIsRedactedField_SelfReferentialMapTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicMapType", 30*time.Second)
}

func TestIsRedactedField_SelfReferentialSliceTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicSliceType", 30*time.Second)
}

func TestIsRedactedField_SelfReferentialPointerTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicPointerType", 30*time.Second)
}

func TestValueContainsRedactTag_SelfReferentialPointerSubFieldTypeTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicPointerNestedType", 30*time.Second)
}

func TestChangeSet_SelfEmbeddedPointerCycleTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "cyclicEmbeddedPointer", 30*time.Second)
}

func TestChangeSet_MutuallyEmbeddedPointerCycleTerminates(t *testing.T) {
    assertChangeProbeExitsCleanly(t, "mutuallyCyclicEmbeddedPointer", 30*time.Second)
}

type finiteChainLeaf struct {
    Leaf string `bun:"leaf"`
}

type finiteChainMiddle struct {
    *finiteChainLeaf
    Middle string `bun:"middle"`
}

type finiteChainRoot struct {
    *finiteChainMiddle
    Name string `bun:"name"`
}

func TestChangeSet_FiniteEmbeddedPointerChainIsStillWalked(t *testing.T) {
    before := &finiteChainRoot{Name: "root", finiteChainMiddle: &finiteChainMiddle{Middle: "middle", finiteChainLeaf: &finiteChainLeaf{Leaf: "leaf-before"}}}
    after := &finiteChainRoot{Name: "root", finiteChainMiddle: &finiteChainMiddle{Middle: "middle", finiteChainLeaf: &finiteChainLeaf{Leaf: "leaf-after"}}}

    changes := ChangeSet(before, after)

    leafChange, found := findChange(changes, "leaf")
    if false == found {
        t.Fatalf("expected the deepest embed of a finite chain to still be diffed, got %+v", changes)
    }
    if "leaf-before" != leafChange.Old || "leaf-after" != leafChange.New {
        t.Fatalf("expected the change from the third node of the chain, got %+v", leafChange)
    }
}

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

func TestChangeSet_RedactsTheCompartmentBoundEncryptedTypes(t *testing.T) {
    type compartmentAccount struct {
        Id            int64                                                             `bun:"id,pk"`
        Iban          encrypt.EncryptedStringFor[changeTestCompartmentRef]              `bun:"iban"`
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

    /* a present nil, not a boxed false, is what separates presence from the nil check */
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
