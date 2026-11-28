package translation

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func TestTranslatorServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.translation.translator" != ServiceTranslator {
        t.Fatalf("expected the registered service name, got %q", ServiceTranslator)
    }
}

func TestTranslatorMustFromResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    expected := NewManager(ManagerConfig{FallbackLocales: []string{"en"}})

    serviceContainer.MustRegister(
        ServiceTranslator,
        func(resolver containercontract.Resolver) (translationcontract.Translator, error) {
            return expected, nil
        },
        container.RegisterOptions{},
    )

    if expected != TranslatorMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != TranslatorMustFromResolver(t.Context(), serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestTranslatorMustFromResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = TranslatorMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "resolver", read: func() { _ = TranslatorMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
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
