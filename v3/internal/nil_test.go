package internal

import (
    "testing"
)

type nilProbe struct {
    value string
}

type nilProbeInterface interface {
    Probe() string
}

func (instance *nilProbe) Probe() string {
    return instance.value
}

func TestIsNilInterface_AnswersForTheUntypedNilAndForEveryNilReferenceKind(t *testing.T) {
    if false == IsNilInterface(nil) {
        t.Fatalf("expected the untyped nil to read as nil")
    }

    var nilPointer *nilProbe
    if false == IsNilInterface(nilPointer) {
        t.Fatalf("expected a nil pointer boxed into an interface to read as nil")
    }

    var nilInterface nilProbeInterface
    if false == IsNilInterface(nilInterface) {
        t.Fatalf("expected a nil interface to read as nil")
    }

    var nilMap map[string]string
    if false == IsNilInterface(nilMap) {
        t.Fatalf("expected a nil map to read as nil")
    }

    var nilSlice []string
    if false == IsNilInterface(nilSlice) {
        t.Fatalf("expected a nil slice to read as nil")
    }

    var nilChannel chan int
    if false == IsNilInterface(nilChannel) {
        t.Fatalf("expected a nil channel to read as nil")
    }

    var nilFunction func()
    if false == IsNilInterface(nilFunction) {
        t.Fatalf("expected a nil function to read as nil")
    }
}

func TestIsNilInterface_LiveValuesAndValueKindsAreNotNil(t *testing.T) {
    if true == IsNilInterface(&nilProbe{value: "alive"}) {
        t.Fatalf("expected a live pointer not to read as nil")
    }

    if true == IsNilInterface(nilProbeInterface(&nilProbe{value: "alive"})) {
        t.Fatalf("expected an interface holding a live pointer not to read as nil")
    }

    if true == IsNilInterface(map[string]string{}) {
        t.Fatalf("expected an empty but allocated map not to read as nil")
    }

    if true == IsNilInterface([]string{}) {
        t.Fatalf("expected an empty but allocated slice not to read as nil")
    }

    for _, probe := range []any{0, "", false, 0.0, nilProbe{}, struct{}{}} {
        if true == IsNilInterface(probe) {
            t.Fatalf("expected the value kind %#v not to read as nil", probe)
        }
    }
}
