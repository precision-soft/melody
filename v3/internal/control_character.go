package internal

import (
    "fmt"
    "strings"
    "unicode/utf8"
)

/* EscapeControlCharacters replaces every control character in the value with its visible escape spelling — the named C0 ones as \n, \r, \t, every other C0 one, DEL and the C1 block as \xNN, and the two Unicode line separators as \uNNNN — so text of unknown origin can be written to a terminal or a line-oriented log without repainting the one or forging records in the other. An embedded escape sequence stays data whichever spelling it uses: the ESC byte that would start it is rendered as \x1b and the single-byte C1 introducer that starts one without ESC is rendered as \x9b. Every rune a reader splitting on Unicode line boundaries counts as a record end — the C0 breaks, NEL and the LINE and PARAGRAPH SEPARATOR — is rendered visibly instead of ending the record it belongs to.

   A byte that is not part of a valid UTF-8 sequence is rendered as \xNN of that byte, whatever its value: the C1 introducer arrives from a client as the raw byte 0x9b at least as readily as in its two-byte encoding — a header value admits it, and a percent-encoded path segment is decoded to it before it reaches a log context — and a walk over runes decoded that byte as U+FFFD, let it through as sent, and, once any other control character forced the rewrite, replaced it with U+FFFD in silence, so the same input was either kept or corrupted by what happened to stand beside it. The result is therefore always valid UTF-8, and a genuine U+FFFD in the input, three valid bytes, passes through as itself. */
func EscapeControlCharacters(value string) string {
    return escapeControlCharacters(value, false)
}

/* EscapeControlCharactersKeepingNewlines is the cell form of EscapeControlCharacters: a newline stays a real line break, because the consumer renders multi-line values on purpose, and every other control character is escaped the same way. */
func EscapeControlCharactersKeepingNewlines(value string) string {
    return escapeControlCharacters(value, true)
}

func escapeControlCharacters(value string, keepNewline bool) string {
    if false == containsControlCharacter(value, keepNewline) {
        return value
    }

    var builder strings.Builder
    builder.Grow(len(value) + 8)

    for index := 0; index < len(value); {
        currentRune, width := utf8.DecodeRuneInString(value[index:])

        if true == isInvalidByte(currentRune, width) {
            builder.WriteString(invalidByteSpelling(value[index]))
            index++

            continue
        }

        index += width

        if false == isEscapedControlRune(currentRune, keepNewline) {
            builder.WriteRune(currentRune)

            continue
        }

        builder.WriteString(controlRuneSpelling(currentRune))
    }

    return builder.String()
}

func containsControlCharacter(value string, keepNewline bool) bool {
    for index := 0; index < len(value); {
        currentRune, width := utf8.DecodeRuneInString(value[index:])

        if true == isInvalidByte(currentRune, width) || true == isEscapedControlRune(currentRune, keepNewline) {
            return true
        }

        index += width
    }

    return false
}

/* isInvalidByte reads the decoder's answer for a byte that starts no valid sequence: the replacement rune over a width of one. A genuine U+FFFD in the text decodes from its three bytes and is ordinary text. */
func isInvalidByte(currentRune rune, width int) bool {
    return utf8.RuneError == currentRune && 1 == width
}

func invalidByteSpelling(invalidByte byte) string {
    return fmt.Sprintf(`\x%02x`, invalidByte)
}

const lineSeparatorRune rune = 0x2028

const paragraphSeparatorRune rune = 0x2029

func isEscapedControlRune(currentRune rune, keepNewline bool) bool {
    if '\n' == currentRune {
        return false == keepNewline
    }

    if (0x20 > currentRune && 0 <= currentRune) || 0x7f == currentRune {
        return true
    }

    /* the C1 block: a terminal decoding UTF-8 acts on these the way it acts on the two-byte ESC sequence each one abbreviates, so a single U+009B repaints a line no ESC ever entered, and U+0085 ends a record for every reader that splits on Unicode line boundaries rather than on \n alone. */
    if 0x80 <= currentRune && 0x9f >= currentRune {
        return true
    }

    /* LINE SEPARATOR and PARAGRAPH SEPARATOR are the only runes outside the control blocks that a Unicode line splitter reads as a record boundary; with them escaped the set covers every such boundary, so one escaped record is never read downstream as two. */
    return lineSeparatorRune == currentRune || paragraphSeparatorRune == currentRune
}

func controlRuneSpelling(currentRune rune) string {
    switch currentRune {
    case '\n':
        return `\n`
    case '\r':
        return `\r`
    case '\t':
        return `\t`
    }

    /* the \xNN spelling holds one byte, so a rune above it takes the four-digit \uNNNN form instead: \x2028 would read as \x20 followed by the digits 28, which is a space and not a separator. */
    if 0xff < currentRune {
        return fmt.Sprintf(`\u%04x`, currentRune)
    }

    return fmt.Sprintf(`\x%02x`, currentRune)
}

/* c1LeadByte is the first byte of the two-byte UTF-8 encoding of U+0080 through U+00BF; the second byte tells the C1 block from the Latin-1 punctuation that follows it. */
const c1LeadByte byte = 0xc2

/* EscapeJsonC1Block rewrites every rune of the C1 block in an encoded json document as its \u00NN escape and leaves every other byte as it was. encoding/json escapes the C0 block, the two Unicode line separators and the html-significant runes, and emits the C1 block raw: the document stays valid json, but written to a terminal or captured in a line-oriented log its bytes act as the control sequences they abbreviate, the way an unescaped C1 rune acts in a text record. The rewrite is exact because the structural characters of json are ASCII, so a C1 rune can stand only inside a string literal, where the escape decodes to the very rune the encoder was given — a consumer reads the same value either way. A byte that is not valid UTF-8 never reaches this door: the encoder has already written it as U+FFFD, which is its documented behaviour and the one loss this rewrite cannot undo. */
func EscapeJsonC1Block(document []byte) []byte {
    if false == containsJsonC1Rune(document) {
        return document
    }

    escaped := make([]byte, 0, len(document)+8)

    for index := 0; index < len(document); index++ {
        if true == isJsonC1RuneAt(document, index) {
            escaped = append(escaped, fmt.Sprintf(`\u00%02x`, document[index+1])...)
            index++

            continue
        }

        escaped = append(escaped, document[index])
    }

    return escaped
}

func containsJsonC1Rune(document []byte) bool {
    for index := 0; index < len(document); index++ {
        if true == isJsonC1RuneAt(document, index) {
            return true
        }
    }

    return false
}

/* isJsonC1RuneAt answers whether the two bytes at the index encode a rune of the C1 block: the lead byte alone also opens the no-break space and the Latin-1 punctuation, which must stay. */
func isJsonC1RuneAt(document []byte, index int) bool {
    if c1LeadByte != document[index] || index+1 >= len(document) {
        return false
    }

    return 0x80 <= document[index+1] && 0x9f >= document[index+1]
}
