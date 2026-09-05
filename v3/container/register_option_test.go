package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestApplyRegisterServiceOptions_DefaultsToAStrictTypeRegistration(t *testing.T) {
    option := applyRegisterServiceOptions(nil)

    if false == option.AlsoRegisterType {
        t.Fatalf("expected a registration to declare its type by default")
    }

    if false == option.TypeRegistrationIsStrict {
        t.Fatalf("expected the type registration to be strict by default")
    }

    if true == option.ReplacesContainerService {
        t.Fatalf("expected no waiver by default")
    }
}

func TestRegisterOptions_EachMovesExactlyItsOwnField(t *testing.T) {
    withoutType := applyRegisterServiceOptions([]containercontract.RegisterOption{WithoutTypeRegistration()})
    if true == withoutType.AlsoRegisterType {
        t.Fatalf("expected the type registration to be opted out of")
    }
    if false == withoutType.TypeRegistrationIsStrict {
        t.Fatalf("expected strictness to be left where it was")
    }

    lenient := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTypeRegistration(false)})
    if false == lenient.AlsoRegisterType {
        t.Fatalf("expected the type registration to be turned back on")
    }
    if true == lenient.TypeRegistrationIsStrict {
        t.Fatalf("expected strictness to be lifted")
    }

    strict := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTypeRegistration(true)})
    if false == strict.AlsoRegisterType || false == strict.TypeRegistrationIsStrict {
        t.Fatalf("expected an explicit strict type registration")
    }

    replacing := applyRegisterServiceOptions([]containercontract.RegisterOption{Replacing()})
    if false == replacing.ReplacesContainerService {
        t.Fatalf("expected the waiver to be declared")
    }
    if false == replacing.AlsoRegisterType || false == replacing.TypeRegistrationIsStrict {
        t.Fatalf("expected Replacing to leave the type registration untouched")
    }

    teardown := applyRegisterServiceOptions([]containercontract.RegisterOption{WithTeardownDependency("service.logger")})
    if 1 != len(teardown.TeardownDependencyNames) || "service.logger" != teardown.TeardownDependencyNames[0] {
        t.Fatalf("expected the declared teardown dependency, got %v", teardown.TeardownDependencyNames)
    }
    if false == teardown.AlsoRegisterType || false == teardown.TypeRegistrationIsStrict || true == teardown.ReplacesContainerService {
        t.Fatalf("expected WithTeardownDependency to leave every other field untouched")
    }
}

/* TestWithTeardownDependency_ComposesAcrossCallsAndArguments pins the promise the option's documentation makes about composing: two declarations add to one list rather than the second replacing the first, and one call naming several services keeps all of them in the order they were written. A replacing fold would silently drop the first collaborator, which is the failure no ordering test can see — the edge that is missing simply never constrains anything. */
func TestWithTeardownDependency_ComposesAcrossCallsAndArguments(t *testing.T) {
    option := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithTeardownDependency("service.logger", "service.metrics"),
        WithTeardownDependency("service.tracer"),
    })

    expected := []string{"service.logger", "service.metrics", "service.tracer"}

    if len(expected) != len(option.TeardownDependencyNames) {
        t.Fatalf("expected %v, got %v", expected, option.TeardownDependencyNames)
    }

    for index, name := range expected {
        if name != option.TeardownDependencyNames[index] {
            t.Fatalf("expected %v, got %v", expected, option.TeardownDependencyNames)
        }
    }
}

func TestApplyRegisterServiceOptions_FoldsInOrderAndSkipsANilOption(t *testing.T) {
    option := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithoutTypeRegistration(),
        nil,
        WithTypeRegistration(false),
    })

    if false == option.AlsoRegisterType {
        t.Fatalf("expected the later option to win over the earlier one")
    }

    if true == option.TypeRegistrationIsStrict {
        t.Fatalf("expected the later option to carry its own strictness")
    }

    reversed := applyRegisterServiceOptions([]containercontract.RegisterOption{
        WithTypeRegistration(false),
        WithoutTypeRegistration(),
    })

    if true == reversed.AlsoRegisterType {
        t.Fatalf("expected the reversed order to end with the type registration opted out of")
    }
}
