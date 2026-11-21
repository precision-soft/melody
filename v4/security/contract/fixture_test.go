package contract

import (
    "encoding/json"
    "testing"
    "unicode"
)

func requireCamelCaseKeys(t *testing.T, value any, path string) {
    t.Helper()

    switch typed := value.(type) {
    case map[string]any:
        for key, nested := range typed {
            if "" != key && true == unicode.IsUpper([]rune(key)[0]) {
                t.Fatalf("expected camelCase keys only, got %q at %s", key, path)
            }

            requireCamelCaseKeys(t, nested, path+"."+key)
        }
    case []any:
        for _, nested := range typed {
            requireCamelCaseKeys(t, nested, path)
        }
    }
}

func marshalledObject(t *testing.T, value any) map[string]any {
    t.Helper()

    payload, marshalErr := json.Marshal(value)
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    decoded := map[string]any{}
    if unmarshalErr := json.Unmarshal(payload, &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    return decoded
}

func fullActorData() *ActorData {
    return &ActorData{
        Identifier:   "user-7",
        Type:         "user",
        Roles:        []string{"ROLE_USER"},
        Attributes:   map[string]string{"Tenant": "acme"},
        Impersonator: &ActorData{Identifier: "admin-1", Type: "user", Roles: []string{"ROLE_ADMIN"}, Attributes: map[string]string{"Desk": "1"}},
    }
}
