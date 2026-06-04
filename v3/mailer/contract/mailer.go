package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Address struct {
    Name  string
    Email string
}

type Message struct {
    From    Address
    To      []Address
    Cc      []Address
    Bcc     []Address
    ReplyTo Address
    Subject string
    Text    string
    Html    string
    Headers map[string]string
}

type Mailer interface {
    Send(runtimeInstance runtimecontract.Runtime, message Message) error
}

type Transport interface {
    Send(runtimeInstance runtimecontract.Runtime, message Message) error
}
