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

/* Receipt is what a delivery answers; fields may be added. */
type Receipt struct {
    /* MessageId is the identifier the transport gave the message, empty when it gave none. */
    MessageId string
}

/* Mailer sends a message and answers its transport's receipt, the zero receipt beside any error. */
type Mailer interface {
    Send(runtimeInstance containercontract.Runtime, message Message) (Receipt, error)
}

type Transport interface {
    Send(runtimeInstance containercontract.Runtime, message Message) (Receipt, error)
}
