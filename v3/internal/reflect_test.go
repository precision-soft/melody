package internal

import (
    "reflect"
    "testing"
)

func TestCanReflectValueBeNil_AnswersForEveryReferenceKind(t *testing.T) {
    var nillableProbe = []any{
        make(chan int),
        func() {},
        map[string]string{},
        &struct{}{},
        []string{},
    }

    for _, probe := range nillableProbe {
        if false == CanReflectValueBeNil(reflect.ValueOf(probe)) {
            t.Fatalf("expected the kind of %T to be reported as nillable", probe)
        }
    }

    /* an interface kind is only reachable through a reflect.Value built from a pointer's element, because reflect.ValueOf unwraps the interface it is handed */
    var interfaceHolder struct {
        field any
    }

    interfaceValue := reflect.ValueOf(&interfaceHolder).Elem().Field(0)
    if reflect.Interface != interfaceValue.Kind() {
        t.Fatalf("expected the probe to carry an interface kind, got %v", interfaceValue.Kind())
    }

    if false == CanReflectValueBeNil(interfaceValue) {
        t.Fatalf("expected an interface kind to be reported as nillable")
    }
}

func TestCanReflectValueBeNil_ValueKindsAreNotNillable(t *testing.T) {
    for _, probe := range []any{0, int64(0), 0.0, "", false, struct{}{}, [2]int{}} {
        if true == CanReflectValueBeNil(reflect.ValueOf(probe)) {
            t.Fatalf("expected the kind of %T not to be reported as nillable", probe)
        }
    }
}
