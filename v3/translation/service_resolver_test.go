package translation

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    translationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

func TestTranslatorServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.translation.translator" != ServiceTranslator {
        t.Fatalf("expected the registered service name, got %q", ServiceTranslator)
    }
}

func TestTranslatorMustFromContainerAndResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer()

    expected := NewManager("en", nil)

    serviceContainer.MustRegister(
        ServiceTranslator,
        func(resolver containercontract.Resolver) (translationcontract.Translator, error) {
            return expected, nil
        },
    )

    if expected != TranslatorMustFromContainer(serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != TranslatorMustFromResolver(serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestTranslatorMustFromContainerAndResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = TranslatorMustFromContainer(container.NewContainer()) }},
        {name: "resolver", read: func() { _ = TranslatorMustFromResolver(container.NewContainer().NewScope()) }},
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("%s: expected the strict reader to panic when nothing is registered", probe.name)
                }
            }()

            probe.read()
        }()
    }
}
