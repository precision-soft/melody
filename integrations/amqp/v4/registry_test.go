package amqp

import (
    "sync"
    "testing"
)

type registryProbeMessage struct {
    Id int
}

type registryOtherMessage struct {
    Id int
}

func TestMessageRegistry_RoundTripsANameToAFreshValueOfItsType(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[registryProbeMessage](registry, "probe")

    name, exists := registry.NameFor(registryProbeMessage{Id: 1})
    if false == exists || "probe" != name {
        t.Fatalf("expected the registered name, got %q exists=%v", name, exists)
    }

    /* New answers a POINTER to a zero value: the decoder unmarshals into it, so handing back the value itself would decode into a copy nobody reads */
    message, built := registry.New("probe")
    if false == built {
        t.Fatal("expected the registered name to build a message")
    }

    typed, isTyped := message.(*registryProbeMessage)
    if false == isTyped {
        t.Fatalf("expected a pointer to the registered type, got %T", message)
    }

    if 0 != typed.Id {
        t.Fatalf("expected a zero value to decode into, got %+v", typed)
    }
}

func TestMessageRegistry_AnswersFalseForAnythingUnregistered(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[registryProbeMessage](registry, "probe")

    if name, exists := registry.NameFor(registryOtherMessage{}); true == exists || "" != name {
        t.Fatalf("expected an unregistered type to have no name, got %q", name)
    }

    if message, built := registry.New("absent"); true == built || nil != message {
        t.Fatalf("expected an unregistered name to build nothing, got %v", message)
    }

    /* the name is keyed on the VALUE type, so a pointer to a registered message is not itself registered */
    if name, exists := registry.NameFor(&registryProbeMessage{}); true == exists || "" != name {
        t.Fatalf("expected a pointer to be a different type, got %q", name)
    }
}

/* re-registering the same pair is how two modules can declare the same message without ordering between them; a name or a type that would come to mean two things is refused, because a wire name resolving to the wrong type decodes silently into the wrong shape */
func TestRegisterMessage_AdmitsTheSamePairAndRefusesEitherCollision(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[registryProbeMessage](registry, "probe")
    RegisterMessage[registryProbeMessage](registry, "probe")

    for _, probe := range []struct {
        name     string
        register func()
    }{
        {name: "name taken by another type", register: func() { RegisterMessage[registryOtherMessage](registry, "probe") }},
        {name: "type registered under another name", register: func() { RegisterMessage[registryProbeMessage](registry, "other") }},
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("%s: expected the collision to be refused", probe.name)
                }
            }()

            probe.register()
        }()
    }
}

/* the registry is read on every publish and every delivery, from the transport's goroutines, while a late module may still be registering: the guard is the mutex, and without it this is the concurrent map access that kills the process */
func TestMessageRegistry_IsSafeUnderConcurrentRegistrationAndReads(t *testing.T) {
    registry := NewMessageRegistry()

    var waitGroup sync.WaitGroup
    waitGroup.Add(3)

    go func() {
        defer waitGroup.Done()

        RegisterMessage[registryProbeMessage](registry, "probe")
    }()

    go func() {
        defer waitGroup.Done()

        _, _ = registry.NameFor(registryProbeMessage{})
    }()

    go func() {
        defer waitGroup.Done()

        _, _ = registry.New("probe")
    }()

    waitGroup.Wait()

    if name, exists := registry.NameFor(registryProbeMessage{}); false == exists || "probe" != name {
        t.Fatalf("expected the registration to have landed, got %q exists=%v", name, exists)
    }
}
