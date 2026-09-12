package mailer

import (
    "sync"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryTransport() *InMemoryTransport {
    return &InMemoryTransport{}
}

/* InMemoryTransport records messages under a mutex. Sent copies the outer message slice; nested message data remains shared with the supplied messages. */
type InMemoryTransport struct {
    mutex sync.Mutex
    sent  []mailercontract.Message
}

func (instance *InMemoryTransport) Send(runtimeInstance runtimecontract.Runtime, message mailercontract.Message) error {
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
