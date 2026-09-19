package contract

import (
    "reflect"
    "testing"
)

/* the contract is the whole of what a command may ask of its run, and the set is deliberately small: every method here is one a command in this tree actually calls, and a method added without a caller is a door the engine adapter has to keep answering forever. Asserted by name so a method silently dropped in a rewrite fails here rather than at the first command that needed it. */
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
