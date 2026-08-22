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
