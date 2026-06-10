package mailer_test

import (
    "bufio"
    "bytes"
    "mime"
    "net/textproto"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/mailer"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestRenderMessage_EncodesEspecialsInNonAsciiDisplayName(t *testing.T) {
    name := "Müller, Inc. (test); <x>"

    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: name, Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: "Hi",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    fromHeader := header.Get("From")

    for _, especial := range []string{",", "(", ")", ";"} {
        if true == strings.Contains(fromHeader, especial) {
            t.Fatalf("non-ASCII From display name leaked RFC 2047 especial %q into a phrase-context encoded-word: %q", especial, fromHeader)
        }
    }

    decoded, decodeErr := new(mime.WordDecoder).DecodeHeader(fromHeader)
    if nil != decodeErr {
        t.Fatalf("decode From: %v", decodeErr)
    }
    if false == strings.Contains(decoded, name) {
        t.Fatalf("display name did not round-trip through the encoded-words; got %q", decoded)
    }
}
