package mailer

import (
    "sync"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

func NewInMemoryTransport() *InMemoryTransport {
    return &InMemoryTransport{}
}

type InMemoryTransport struct {
    mutex sync.Mutex
    sent  []mailercontract.Message
}

func (instance *InMemoryTransport) Send(runtimeInstance containercontract.Runtime, message mailercontract.Message) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.sent = append(instance.sent, message)

    return nil
}

func (instance *InMemoryTransport) Sent() []mailercontract.Message {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]mailercontract.Message{}, instance.sent...)
}

var _ mailercontract.Transport = (*InMemoryTransport)(nil)
