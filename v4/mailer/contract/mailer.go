package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Address struct {
    Name  string
    Email string
}

type Attachment struct {
    Filename    string
    ContentType string
    Content     []byte
    /* when ContentId is non-empty the attachment is embedded inline (Content-Disposition: inline) inside a multipart/related part so an HTML body can reference it as <img src="cid:..."> */
    ContentId string
}

type Message struct {
    From        Address
    To          []Address
    Cc          []Address
    Bcc         []Address
    ReplyTo     Address
    Subject     string
    Text        string
    Html        string
    Headers     map[string]string
    Attachments []Attachment
}

type Mailer interface {
    Send(runtimeInstance containercontract.Runtime, message Message) error
}

type Transport interface {
    Send(runtimeInstance containercontract.Runtime, message Message) error
}
