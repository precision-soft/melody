package bag

import (
	"errors"
	"testing"
	"time"

	"github.com/precision-soft/melody/internal"
)

func TestBagString_StringOrDefault_HasNonEmptyString(t *testing.T) {
	parameterBag := NewParameterBag()

	_, exists := String(parameterBag, "name")
	if true == exists {
		t.Fatalf("expected exists to be false")
	}

	if "default" != StringOrDefault(parameterBag, "name", "default") {
		t.Fatalf("expected default value")
	}

	parameterBag.Set("name", "   ")
	if false != HasNonEmptyString(parameterBag, "name") {
		t.Fatalf("expected HasNonEmptyString to be false for whitespace")
	}

	parameterBag.Set("name", "value")
	value, exists := String(parameterBag, "name")
	if false == exists {
		t.Fatalf("expected exists to be true")
	}
	if "value" != value {
		t.Fatalf("expected value %q, got %q", "value", value)
	}
}

func TestBagInt_ConversionsAndErrors(t *testing.T) {
	parameterBag := NewParameterBag()

	_, exists, err := Int(parameterBag, "n")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if true == exists {
		t.Fatalf("expected exists false")
	}

	parameterBag.Set("n", int64(10))
	value, exists, err := Int(parameterBag, "n")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if 10 != value {
		t.Fatalf("expected %d, got %d", 10, value)
	}

	parameterBag.Set("n", " 42 ")
	value, exists, err = Int(parameterBag, "n")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if 42 != value {
		t.Fatalf("expected %d, got %d", 42, value)
	}

	parameterBag.Set("n", "not-a-number")
	_, exists, err = Int(parameterBag, "n")
	if false == exists {
		t.Fatalf("expected exists true even when conversion fails")
	}
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestBagBool_ConversionsAndErrors(t *testing.T) {
	parameterBag := NewParameterBag()

	parameterBag.Set("b", true)
	value, exists, err := Bool(parameterBag, "b")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if true != value {
		t.Fatalf("expected true")
	}

	parameterBag.Set("b", " false ")
	value, exists, err = Bool(parameterBag, "b")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if false != value {
		t.Fatalf("expected false")
	}

	parameterBag.Set("b", " yes ")
	value, exists, err = Bool(parameterBag, "b")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if true != value {
		t.Fatalf("expected true")
	}

	parameterBag.Set("b", " off ")
	value, exists, err = Bool(parameterBag, "b")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if false != value {
		t.Fatalf("expected false")
	}

	parameterBag.Set("b", "not-bool")
	_, exists, err = Bool(parameterBag, "b")
	if false == exists {
		t.Fatalf("expected exists true even when conversion fails")
	}
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestBagFloat64_ConversionsAndErrors(t *testing.T) {
	parameterBag := NewParameterBag()

	parameterBag.Set("f", float32(1.5))
	value, exists, err := Float64(parameterBag, "f")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if 1.5 != value {
		t.Fatalf("expected %v, got %v", 1.5, value)
	}

	parameterBag.Set("f", " 2.25 ")
	value, exists, err = Float64(parameterBag, "f")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if 2.25 != value {
		t.Fatalf("expected %v, got %v", 2.25, value)
	}

	parameterBag.Set("f", "not-float")
	_, exists, err = Float64(parameterBag, "f")
	if false == exists {
		t.Fatalf("expected exists true even when conversion fails")
	}
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestBagDuration_ConversionsAndErrors(t *testing.T) {
	parameterBag := NewParameterBag()

	parameterBag.Set("d", time.Second)
	value, exists, err := Duration(parameterBag, "d")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if time.Second != value {
		t.Fatalf("expected %v, got %v", time.Second, value)
	}

	parameterBag.Set("d", " 150ms ")
	value, exists, err = Duration(parameterBag, "d")
	if nil != err {
		t.Fatalf("unexpected error: %v", err)
	}
	if false == exists {
		t.Fatalf("expected exists true")
	}
	if 150*time.Millisecond != value {
		t.Fatalf("expected %v, got %v", 150*time.Millisecond, value)
	}

	parameterBag.Set("d", "not-duration")
	_, exists, err = Duration(parameterBag, "d")
	if false == exists {
		t.Fatalf("expected exists true even when conversion fails")
	}
	if nil == err {
		t.Fatalf("expected error")
	}
}

func TestCreateConversionError_MessageAndContext(t *testing.T) {
	err := internal.ParseError("n", "int", "not-a-number", errors.New("cause"))
	if nil == err {
		t.Fatalf("expected error")
	}
	if "parameter is not a valid 'int'" != err.Message() {
		t.Fatalf("unexpected message: %q", err.Message())
	}
	if nil == err.Context() {
		t.Fatalf("expected context")
	}
	if "n" != err.Context()["parameterName"] {
		t.Fatalf("expected parameterName in context")
	}

	err = internal.ParseError("n", "int", 123, nil)
	if nil == err {
		t.Fatalf("expected error")
	}
	if "parameter is not a 'int'" != err.Message() {
		t.Fatalf("unexpected message: %q", err.Message())
	}
}
