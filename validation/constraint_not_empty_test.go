package validation

import (
    "testing"
)

/* @info notEmpty is the constraint an application reaches for on a required collection, and nothing had ever driven it: it walks through pointers and interfaces before it measures, which is what lets a *[]string declared optional-but-present be judged on what it points at rather than on the pointer being non-nil. */
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

/* @info the four measurable kinds are the whole of what the constraint accepts, and each has to be refused when it is empty — a kind quietly falling through to the accepting branch would declare a required field satisfied by nothing. */
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

/* @info a nil — untyped, or a nil pointer, or a nil interface — is the absence the constraint exists to refuse, and it must be refused as EMPTY rather than as the wrong type: a required field left out and a required field filled with a number are different mistakes, and the code is what a client acts on. */
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

/* @info a kind that cannot be measured is refused by NAME rather than accepted, which is the symmetry the validation session established across the constraints: a number handed to a collection constraint is a declaration mistake and has to say so, not pass. */
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
