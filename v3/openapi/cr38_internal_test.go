package openapi

import (
    "testing"
)

func TestApplyValidation_BareStringConstraintsMirrorValidatorDefaults(t *testing.T) {
    minSchema := &Schema{Type: "string"}
    applyValidation(minSchema, "min")
    if nil == minSchema.MinLength || 1 != *minSchema.MinLength {
        t.Fatalf("expected bare min to advertise minLength 1, got %v", minSchema.MinLength)
    }

    maxSchema := &Schema{Type: "string"}
    applyValidation(maxSchema, "max")
    if nil == maxSchema.MaxLength || 100 != *maxSchema.MaxLength {
        t.Fatalf("expected bare max to advertise maxLength 100, got %v", maxSchema.MaxLength)
    }
}

func TestApplyValidation_BareNumericConstraintsMirrorValidatorDefaults(t *testing.T) {
    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan")
    if nil == greaterThanSchema.Minimum || 0 != *greaterThanSchema.Minimum {
        t.Fatalf("expected bare greaterThan to advertise minimum 0, got %v", greaterThanSchema.Minimum)
    }
    if nil == greaterThanSchema.ExclusiveMinimum || false == *greaterThanSchema.ExclusiveMinimum {
        t.Fatalf("expected bare greaterThan to advertise an exclusive minimum")
    }

    lessThanSchema := &Schema{Type: "number"}
    applyValidation(lessThanSchema, "lessThan")
    if nil == lessThanSchema.Maximum || 0 != *lessThanSchema.Maximum {
        t.Fatalf("expected bare lessThan to advertise maximum 0, got %v", lessThanSchema.Maximum)
    }
    if nil == lessThanSchema.ExclusiveMaximum || false == *lessThanSchema.ExclusiveMaximum {
        t.Fatalf("expected bare lessThan to advertise an exclusive maximum")
    }
}

func TestApplyValidation_ValuedConstraintsStillHonourTheirValue(t *testing.T) {
    minSchema := &Schema{Type: "string"}
    applyValidation(minSchema, "min(value=3)")
    if nil == minSchema.MinLength || 3 != *minSchema.MinLength {
        t.Fatalf("expected min(value=3) to advertise minLength 3, got %v", minSchema.MinLength)
    }

    greaterThanSchema := &Schema{Type: "integer"}
    applyValidation(greaterThanSchema, "greaterThan(value=5)")
    if nil == greaterThanSchema.Minimum || 5 != *greaterThanSchema.Minimum {
        t.Fatalf("expected greaterThan(value=5) to advertise minimum 5, got %v", greaterThanSchema.Minimum)
    }
}
