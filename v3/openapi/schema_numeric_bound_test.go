package openapi

import (
    "reflect"
    "testing"
)

func TestApplyValidation_GreaterLessThanBoundMatchesValidatorIntegerTruncation(t *testing.T) {
    floatType := reflect.TypeOf(float64(0))
    structType := reflect.StructOf([]reflect.StructField{
        {Name: "Price", Type: floatType, Tag: `json:"price" validate:"greaterThan=9.99"`},
        {Name: "Count", Type: floatType, Tag: `json:"count" validate:"lessThan=130.9"`},
    })

    schema := buildSchema(structType, map[string]*Schema{}, map[reflect.Type]string{}, map[reflect.Type]bool{})

    price := schema.Properties["price"]
    if nil == price || nil == price.Minimum {
        t.Fatalf("expected a minimum on price, got %#v", price)
    }
    if 9.0 != *price.Minimum {
        t.Fatalf("the runtime validator truncates greaterThan=9.99 to an integer bound (>9); the spec minimum must match, got %v", *price.Minimum)
    }

    count := schema.Properties["count"]
    if nil == count || nil == count.Maximum {
        t.Fatalf("expected a maximum on count, got %#v", count)
    }
    if 130.0 != *count.Maximum {
        t.Fatalf("the runtime validator truncates lessThan=130.9 to an integer bound (<130); the spec maximum must match, got %v", *count.Maximum)
    }
}
