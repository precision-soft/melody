package bag

import (
	"net/url"
	"testing"
)

func TestParameterBagSetGetHas(t *testing.T) {
	parameterBag := NewParameterBag()

	parameterBag.Set("name", "value")

	value, exists := parameterBag.Get("name")
	if false == exists {
		t.Fatalf("expected parameter to exist")
	}

	if "value" != value {
		t.Fatalf("expected value 'value', got %v", value)
	}

	if false == parameterBag.Has("name") {
		t.Fatalf("expected Has(name) to return true")
	}

	if true == parameterBag.Has("missing") {
		t.Fatalf("expected Has(missing) to return false")
	}
}

func TestParameterBagOverwriteValue(t *testing.T) {
	parameterBag := NewParameterBag()

	parameterBag.Set("key", "value1")
	parameterBag.Set("key", "value2")

	value, exists := parameterBag.Get("key")
	if false == exists {
		t.Fatalf("expected parameter to exist")
	}

	if "value2" != value {
		t.Fatalf("expected overwritten value")
	}
}

func TestNewParameterBagFromValuesDeepCopy(t *testing.T) {
	values := url.Values{}
	values.Add("tag", "a")
	values.Add("tag", "b")

	parameterBag := NewParameterBagFromValues(values)

	original := values["tag"]
	original[0] = "modified"

	valueAny, exists := parameterBag.Get("tag")
	if false == exists {
		t.Fatalf("expected tag to exist")
	}

	sliceValue, ok := valueAny.([]string)
	if false == ok {
		t.Fatalf("expected []string, got %T", valueAny)
	}

	if "a" != sliceValue[0] {
		t.Fatalf("expected NewParameterBagFromValues to deep copy url.Values content")
	}
}
