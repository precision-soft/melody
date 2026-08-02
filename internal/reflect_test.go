package internal

import (
    "reflect"
    "testing"
)

/* @info the deep copy asks this before it dereferences anything, so a kind wrongly reported as nillable is a nil check on a value that cannot be nil — harmless — while a nillable kind wrongly reported as not is a dereference of nil in the middle of a copy the caller believes is safe. The six reference kinds are named one by one because the answer is a switch, and a case dropped from it is invisible to any test that probes only one of them. */
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

/* @info the other face: a value kind reported as nillable would send the copy down a nil check that cannot fire, and the default branch is what keeps the six cases above meaningful. */
func TestCanReflectValueBeNil_ValueKindsAreNotNillable(t *testing.T) {
    for _, probe := range []any{0, int64(0), 0.0, "", false, struct{}{}, [2]int{}} {
        if true == CanReflectValueBeNil(reflect.ValueOf(probe)) {
            t.Fatalf("expected the kind of %T not to be reported as nillable", probe)
        }
    }
}
