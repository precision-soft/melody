package audit_test

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
)

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

    changes := audit.ChangeSet(before, after)

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

const redactedValueLiteral = "<redacted>"

type docWithMap struct {
    Id   int64          `bun:"id,pk"`
    Meta map[string]any `bun:"meta"`
}

func TestChangeSet_SelfReferentialMapDoesNotStackOverflow(t *testing.T) {
    cyclic := map[string]any{}
    cyclic["self"] = cyclic

    before := docWithMap{Id: 1}
    after := docWithMap{Id: 1, Meta: cyclic}

    changes := audit.ChangeSet(before, after)
    if 0 == len(changes) {
        t.Fatalf("expected a change for the meta field")
    }
}
