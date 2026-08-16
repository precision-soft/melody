package validation

import (
    "testing"
)

func TestNotEmpty_MeasuresThroughPointersAndInterfaces(t *testing.T) {
    constraint := NewNotEmpty()

    filled := []string{"one"}

    if nil != constraint.Validate(filled, "field") {
        t.Fatalf("expected a filled slice to pass")
    }

    if nil != constraint.Validate(&filled, "field") {
        t.Fatalf("expected a pointer to a filled slice to pass")
    }

    empty := []string{}

    if nil == constraint.Validate(&empty, "field") {
        t.Fatalf("expected a pointer to an empty slice to be refused")
    }

    var holder any = filled

    if nil != constraint.Validate(holder, "field") {
        t.Fatalf("expected an interface holding a filled slice to pass")
    }
}

func TestNotEmpty_RefusesEveryEmptyMeasurableKind(t *testing.T) {
    constraint := NewNotEmpty()

    for _, probe := range []any{"", []string{}, map[string]string{}, [0]int{}} {
        validationErr := constraint.Validate(probe, "field")
        if nil == validationErr {
            t.Fatalf("expected the empty %T to be refused", probe)
        }

        if "value must not be empty" != validationErr.Message() {
            t.Fatalf("unexpected message for %T: %q", probe, validationErr.Message())
        }

        if ConstraintNotEmptyErrorEmpty != validationErr.Code() {
            t.Fatalf("unexpected code for %T: %q", probe, validationErr.Code())
        }
    }

    for _, probe := range []any{"x", []string{"x"}, map[string]string{"k": "v"}, [1]int{1}} {
        if nil != constraint.Validate(probe, "field") {
            t.Fatalf("expected the filled %T to pass", probe)
        }
    }
}

func TestNotEmpty_EveryShapeOfNilIsRefusedAsEmpty(t *testing.T) {
    constraint := NewNotEmpty()

    var nilSlice []string
    var nilMap map[string]string
    var nilPointer *string

    for _, probe := range []any{nil, nilSlice, nilMap, nilPointer} {
        validationErr := constraint.Validate(probe, "field")
        if nil == validationErr {
            t.Fatalf("expected the nil %T to be refused", probe)
        }

        if "value must not be empty" != validationErr.Message() {
            t.Fatalf("unexpected message for the nil %T: %q", probe, validationErr.Message())
        }
    }
}

func TestNotEmpty_AnUnmeasurableKindIsRefusedForWhatItIs(t *testing.T) {
    constraint := NewNotEmpty()

    for _, probe := range []any{42, 2.5, true, struct{}{}} {
        validationErr := constraint.Validate(probe, "field")
        if nil == validationErr {
            t.Fatalf("expected the unmeasurable %T to be refused", probe)
        }

        if "value must be a string/array/slice/map" != validationErr.Message() {
            t.Fatalf("unexpected message for %T: %q", probe, validationErr.Message())
        }
    }
}
