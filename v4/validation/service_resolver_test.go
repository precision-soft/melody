package validation

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestValidatorFromResolver_ReturnsNilWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    validatorInstance := ValidatorFromResolver(t.Context(), serviceContainer)
    if nil != validatorInstance {
        t.Fatalf("expected nil")
    }
}

func TestValidatorMustFromResolver_PanicsWhenMissing(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = ValidatorMustFromResolver(t.Context(), serviceContainer)
}

func TestValidatorFromResolver_AnswersTheRegisteredValidator(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registeredValidator := NewValidator(ValidatorConfig{})

    registerErr := serviceContainer.Register(
        ServiceValidator,
        func(resolver containercontract.Resolver) (*Validator, error) {
            return registeredValidator, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registeredValidator != ValidatorFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered validator to be handed back")
    }

    if registeredValidator != ValidatorMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the must resolver to hand back the same instance")
    }
}
