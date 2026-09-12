package validation

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

func TestValidatorFromContainer_AnswersTheRegisteredValidator(t *testing.T) {
    serviceContainer := container.NewContainer()

    registeredValidator := NewValidator()

    registerErr := serviceContainer.Register(
        ServiceValidator,
        func(resolver containercontract.Resolver) (*Validator, error) {
            return registeredValidator, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registeredValidator != ValidatorFromContainer(serviceContainer) {
        t.Fatalf("expected the registered validator to be handed back")
    }

    if registeredValidator != ValidatorMustFromContainer(serviceContainer) {
        t.Fatalf("expected the must resolver to hand back the same instance")
    }
}
