package mailer

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

/* the service NAME is the whole wiring: a name that drifts from the one the module registers under leaves every reader blind while everything still compiles, so the constant is asserted by value rather than through itself. */
func TestMailerServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.mailer.mailer" != ServiceMailer {
        t.Fatalf("expected the registered service name, got %q", ServiceMailer)
    }
}

func TestMailerMustFromContainerAndResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer()

    expected := NewManager(NewInMemoryTransport())

    serviceContainer.MustRegister(
        ServiceMailer,
        func(resolver containercontract.Resolver) (mailercontract.Mailer, error) {
            return expected, nil
        },
    )

    if expected != MailerMustFromContainer(serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    /* the resolver-taking door is what a scoped service reads through: a scope is a Resolver and not a Container, so the two doors are not interchangeable at the call site */
    if expected != MailerMustFromResolver(serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

/* the strict readers are the boot-time ones: they panic rather than hand back a nil the caller would dereference later, at a point where nothing names the missing registration */
func TestMailerMustFromContainerAndResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = MailerMustFromContainer(container.NewContainer()) }},
        {name: "resolver", read: func() { _ = MailerMustFromResolver(container.NewContainer().NewScope()) }},
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
