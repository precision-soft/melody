package container

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type bootFailureModeProbe struct{}

type unregisteredTypeProbe struct{}

func bootFailureModeProvider(resolver containercontract.Resolver) (*bootFailureModeProbe, error) {
    return &bootFailureModeProbe{}, nil
}

func TestBootFailureMode_EveryRegistrationDoorRefusesAModeWithoutBuiltAtBootAndAnUnknownMode(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        options  RegisterOptions
        expected error
    }{
        {"a mode without built at boot", RegisterOptions{BootFailure: BootFailureFailsBoot}, ErrBootFailureWithoutBuiltAtBoot},
        {"an unknown mode", RegisterOptions{BuiltAtBoot: true, BootFailure: BootFailureFailsBoot + 1}, ErrBootFailureModeUnknown},
        {"an unknown mode without built at boot", RegisterOptions{BootFailure: BootFailureFailsBoot + 1}, ErrBootFailureModeUnknown},
    } {
        serviceContainer := NewContainer(ContainerConfig{})

        if registerErr := serviceContainer.Register("app.mode", bootFailureModeProvider, testCase.options); false == errors.Is(registerErr, testCase.expected) {
            t.Fatalf("%s: expected the container to refuse with %v, got %v", testCase.name, testCase.expected, registerErr)
        }

        if 0 != len(serviceContainer.(*container).BuiltAtBootRegistrations()) {
            t.Fatalf("%s: expected nothing recorded for a refused registration", testCase.name)
        }

        if false == testCase.options.BuiltAtBoot {
            if registerErr := serviceContainer.RegisterScoped("app.mode.scoped", bootFailureModeProvider, testCase.options); false == errors.Is(registerErr, testCase.expected) {
                t.Fatalf("%s: expected the scoped registrar to refuse with %v, got %v", testCase.name, testCase.expected, registerErr)
            }

            scopeInstance := serviceContainer.NewScope()
            if registerErr := scopeInstance.RegisterScoped("app.mode.scope", bootFailureModeProvider, testCase.options); false == errors.Is(registerErr, testCase.expected) {
                t.Fatalf("%s: expected the scope to refuse with %v, got %v", testCase.name, testCase.expected, registerErr)
            }
        }
    }
}

func TestBootFailureMode_AScopedRegistrationBuiltAtBootIsRefusedAsScopedFirst(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    options := RegisterOptions{BuiltAtBoot: true, BootFailure: BootFailureFailsBoot + 1}

    if registerErr := serviceContainer.RegisterScoped("app.mode.scoped", bootFailureModeProvider, options); false == errors.Is(registerErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected the scoped registrar's own refusal, got %v", registerErr)
    }

    if registerErr := serviceContainer.NewScope().RegisterScoped("app.mode.scope", bootFailureModeProvider, options); false == errors.Is(registerErr, ErrScopedServiceAtBoot) {
        t.Fatalf("expected the scope's own refusal, got %v", registerErr)
    }
}

func TestBootFailureMode_TheModeNamesItself(t *testing.T) {
    for mode, expected := range map[BootFailureMode]string{
        BootFailureWarns:         "warns",
        BootFailureFailsBoot:     "failsBoot",
        BootFailureFailsBoot + 1: "unknown",
    } {
        if expected != mode.String() {
            t.Fatalf("expected %d to read %q, got %q", mode, expected, mode.String())
        }
    }
}

func TestErrServiceNotRegistered_IsTheCauseOfEveryNotRegisteredRefusal(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if _, getErr := serviceContainer.Get(t.Context(), "app.never.registered"); false == errors.Is(getErr, ErrServiceNotRegistered) {
        t.Fatalf("expected the container's refusal by name to carry the sentinel, got %v", getErr)
    }

    if _, getErr := FromResolverByType[*unregisteredTypeProbe](t.Context(), serviceContainer); false == errors.Is(getErr, ErrServiceNotRegistered) {
        t.Fatalf("expected the container's refusal by type to carry the sentinel, got %v", getErr)
    }

    if _, getErr := serviceContainer.NewScope().Get(t.Context(), "app.never.registered"); false == errors.Is(getErr, ErrServiceNotRegistered) {
        t.Fatalf("expected the scope's refusal by name to carry the sentinel, got %v", getErr)
    }
}
