package contract

import (
    "encoding/json"
    "reflect"
    "testing"
    "time"
)

func TestClaims_MarshalsWithCamelCaseKeysOnly(t *testing.T) {
    claims := Claims{
        UserIdentifier:   "user-7",
        Roles:            []string{"ROLE_USER"},
        IssuedAt:         time.Unix(1000, 0).UTC(),
        DeviceIdentifier: "phone",
        Scope:            map[string]any{"tenant": "acme"},
        Attributes:       map[string]any{"name": "Alice"},
        OriginatingActor: fullActorData(),
    }

    decoded := marshalledObject(t, claims)
    delete(decoded["attributes"].(map[string]any), "name")
    delete(decoded["originatingActor"].(map[string]any), "attributes")
    delete(decoded["originatingActor"].(map[string]any)["impersonator"].(map[string]any), "attributes")

    if 7 != len(marshalledObject(t, claims)) {
        t.Fatalf("expected the seven keys of the claims, got %v", marshalledObject(t, claims))
    }

    requireCamelCaseKeys(t, decoded, "claims")
}

func TestClaims_DecodesTheLegacyPascalCaseSpelling(t *testing.T) {
    legacy := `{"UserIdentifier":"user-7","Roles":["ROLE_USER"],"IssuedAt":"1970-01-01T00:16:40Z","DeviceIdentifier":"phone","Scope":{"tenant":"acme"},"Attributes":{"name":"Alice"},"OriginatingActor":{"Identifier":"user-8","Type":"user"}}`

    decoded := Claims{}
    if unmarshalErr := json.Unmarshal([]byte(legacy), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    expected := Claims{
        UserIdentifier:   "user-7",
        Roles:            []string{"ROLE_USER"},
        IssuedAt:         time.Unix(1000, 0).UTC(),
        DeviceIdentifier: "phone",
        Scope:            map[string]any{"tenant": "acme"},
        Attributes:       map[string]any{"name": "Alice"},
        OriginatingActor: &ActorData{Identifier: "user-8", Type: "user"},
    }

    if false == reflect.DeepEqual(expected, decoded) {
        t.Fatalf("expected every legacy key read, got %+v", decoded)
    }
}
