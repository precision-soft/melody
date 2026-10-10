package internal

import (
    "fmt"
    "strings"
    "unicode/utf8"
)

/* EscapeControlCharacters replaces every control character with its visible escape — \n, \r, \t, \xNN for the other C0 ones, DEL and the C1 block, \uNNNN for the two Unicode line separators — so text of unknown origin cannot repaint a terminal or forge a log record. A byte that is not valid UTF-8 is rendered as \xNN of that byte, so the result is always valid UTF-8; a genuine U+FFFD passes through. */
func EscapeControlCharacters(value string) string {
    return escapeControlCharacters(value, false)
}

/* EscapeControlCharactersKeepingNewlines is EscapeControlCharacters with a newline kept as a real line break, for consumers that render multi-line values. */
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

    /* the C1 block: a terminal acts on these like the ESC sequences they abbreviate, and U+0085 ends a record for a Unicode line splitter */
    if 0x80 <= currentRune && 0x9f >= currentRune {
        return true
    }

    /* LINE SEPARATOR and PARAGRAPH SEPARATOR are the only runes outside the control blocks that a Unicode line splitter reads as a record boundary */
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

    /* \xNN holds one byte, so a larger rune takes \uNNNN */
    if 0xff < currentRune {
        return fmt.Sprintf(`\u%04x`, currentRune)
    }

    return fmt.Sprintf(`\x%02x`, currentRune)
}

/* c1LeadByte is the first byte of the two-byte UTF-8 encoding of U+0080 through U+00BF; the second byte tells the C1 block from the Latin-1 punctuation that follows it. */
const c1LeadByte byte = 0xc2

/* EscapeJsonC1Block rewrites every C1 rune of an encoded json document as its \u00NN escape and leaves every other byte as it was; encoding/json emits the C1 block raw. The rewrite is exact because a C1 rune can only stand inside a string literal. */
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
