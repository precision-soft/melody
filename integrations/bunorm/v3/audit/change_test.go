package audit_test

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

type product struct {
    Id    int64  `bun:"id,pk"`
    Name  string `bun:"name"`
    Price int    `bun:"price"`
}

func findChange(changes []audit.Change, field string) (audit.Change, bool) {
    for _, change := range changes {
        if field == change.Field {
            return change, true
        }
    }
    return audit.Change{}, false
}

func TestChangeSet_UpdateCapturesOnlyChangedFields(t *testing.T) {
    before := product{Id: 1, Name: "old", Price: 10}
    after := product{Id: 1, Name: "new", Price: 10}

    changes := audit.ChangeSet(before, after)

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
    changes := audit.ChangeSet(nil, product{Id: 1, Name: "fresh", Price: 5})

    nameChange, found := findChange(changes, "name")
    if false == found || nil != nameChange.Old || "fresh" != nameChange.New {
        t.Fatalf("unexpected insert change: %+v", nameChange)
    }
}

func TestChangeSet_DeleteHasOldOnly(t *testing.T) {
    changes := audit.ChangeSet(product{Id: 1, Name: "gone", Price: 5}, nil)

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

    changes := audit.ChangeSet(before, after)

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

    /** Marshalling the changes is exactly what the recorder writes to the audit table, so the
        plaintext secret must not survive into that serialized form. */
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

func TestRegistry_RejectsInvalidDefaultTableName(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for an invalid default table name")
        }
    }()

    audit.NewRegistry("audit; DROP TABLE users")
}

func TestRegistry_RejectsInvalidEntityTableName(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for an invalid entity table name")
        }
    }()

    audit.NewRegistry("melody_audit").Register("order", audit.EntityOptions{Table: "orders`; DROP"})
}

func TestChangeSet_CapturesPromotedEmbeddedStructFields(t *testing.T) {
    before := orderRow{EmbeddedAuditFields: EmbeddedAuditFields{Status: "open", UpdatedBy: "alice"}, Id: 1, Total: 100}
    after := orderRow{EmbeddedAuditFields: EmbeddedAuditFields{Status: "closed", UpdatedBy: "alice"}, Id: 1, Total: 150}

    changes := audit.ChangeSet(before, after)

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

    changes := audit.ChangeSet(before, after)

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
