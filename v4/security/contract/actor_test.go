package contract

import (
    "encoding/json"
    "reflect"
    "testing"
)

func TestActorData_MarshalsWithCamelCaseKeysOnly(t *testing.T) {
    decoded := marshalledObject(t, fullActorData())
    delete(decoded, "attributes")
    delete(decoded["impersonator"].(map[string]any), "attributes")

    if 5 != len(marshalledObject(t, fullActorData())) {
        t.Fatalf("expected the five keys of the actor, got %v", marshalledObject(t, fullActorData()))
    }

    requireCamelCaseKeys(t, decoded, "actor")
}

func TestActorData_DecodesTheLegacyPascalCaseSpelling(t *testing.T) {
    legacy := `{"Identifier":"user-7","Type":"user","Roles":["ROLE_USER"],"Attributes":{"Tenant":"acme"},"Impersonator":{"Identifier":"admin-1","Type":"user","Roles":["ROLE_ADMIN"],"Attributes":{"Desk":"1"}}}`

    decoded := ActorData{}
    if unmarshalErr := json.Unmarshal([]byte(legacy), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if false == reflect.DeepEqual(*fullActorData(), decoded) {
        t.Fatalf("expected every legacy key read, got %+v", decoded)
    }
}
