package contract

import (
    "reflect"
    "testing"
)

func TestContext_DeclaresExactlyTheReadersACommandNeeds(t *testing.T) {
    contextType := reflect.TypeOf((*Context)(nil)).Elem()

    expected := map[string]bool{
        "String":      true,
        "Bool":        true,
        "Int":         true,
        "StringSlice": true,
        "IsSet":       true,
        "Arguments":   true,
        "Writer":      true,
    }

    if len(expected) != contextType.NumMethod() {
        t.Fatalf("expected %d methods, got %d", len(expected), contextType.NumMethod())
    }

    for index := 0; index < contextType.NumMethod(); index++ {
        methodName := contextType.Method(index).Name
        if false == expected[methodName] {
            t.Fatalf("unexpected method %q on the context contract", methodName)
        }
    }
}
