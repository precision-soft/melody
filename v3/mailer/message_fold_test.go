package mailer

import (
    "strings"
    "testing"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestRenderMessage_LongUnfoldableAddressStaysWithinHardLineLimit(t *testing.T) {
    longEmail := "user@" + strings.Repeat("x", 1200) + ".example.com"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: longEmail},
        To:      []mailercontract.Address{{Email: "recipient@example.com"}},
        Subject: "Test",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("RenderMessage returned an error: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), lineBreak) {
        if len(line) > maxHardHeaderLineLength {
            t.Fatalf(
                "emitted a %d-octet line, exceeding the RFC 5322 %d-octet hard limit: %.80q...",
                len(line),
                maxHardHeaderLineLength,
                line,
            )
        }
    }
}

func TestFoldHeaderLine_HardWrapPreservesValueBytes(t *testing.T) {
    word := strings.Repeat("a", 3000)
    folded := foldHeaderLine("X-Token", word)

    var rebuilt strings.Builder
    for index, line := range strings.Split(folded, lineBreak) {
        segment := line
        if 0 == index {
            segment = strings.TrimPrefix(line, "X-Token:")
        }
        rebuilt.WriteString(strings.TrimPrefix(segment, " "))

        if len(line) > maxHardHeaderLineLength {
            t.Fatalf("folded line %d is %d octets, exceeding the %d-octet hard limit", index, len(line), maxHardHeaderLineLength)
        }
    }

    if rebuilt.String() != word {
        t.Fatalf("hard-wrapping corrupted the value: rebuilt %d bytes, expected %d", rebuilt.Len(), len(word))
    }
}
