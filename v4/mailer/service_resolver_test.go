package mailer

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

func TestMailerServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.mailer.mailer" != ServiceMailer {
        t.Fatalf("expected the registered service name, got %q", ServiceMailer)
    }
}

func TestMailerMustFromResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    expected := NewManager(NewInMemoryTransport(), ManagerConfig{})

    serviceContainer.MustRegister(
        ServiceMailer,
        func(resolver containercontract.Resolver) (mailercontract.Mailer, error) {
            return expected, nil
        },
        container.RegisterOptions{},
    )

    if expected != MailerMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != MailerMustFromResolver(t.Context(), serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestMailerMustFromResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = MailerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "resolver", read: func() { _ = MailerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
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
