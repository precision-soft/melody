package validation

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
)

func TestValidatorFromContainer_ReturnsNilWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer()

    validatorInstance := ValidatorFromContainer(serviceContainer)
    if nil != validatorInstance {
        t.Fatalf("expected nil")
    }
}

func TestValidatorMustFromContainer_PanicsWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = ValidatorMustFromContainer(serviceContainer)
}
