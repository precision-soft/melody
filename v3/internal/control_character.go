package internal

import (
    "fmt"
    "strings"
    "unicode/utf8"
)

/* EscapeControlCharacters renders control characters, Unicode line separators and invalid UTF-8 bytes as visible escapes. It prevents terminal control and forged line boundaries while preserving ordinary Unicode, including a valid replacement rune. Its result is valid UTF-8. */
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

    if 0x80 <= currentRune && 0x9f >= currentRune {
        return true
    }

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

    if 0xff < currentRune {
        return fmt.Sprintf(`\u%04x`, currentRune)
    }

    return fmt.Sprintf(`\x%02x`, currentRune)
}

const c1LeadByte byte = 0xc2

/* EscapeJsonC1Block replaces C1 runes in an encoded JSON document with equivalent Unicode escapes, preserving all other bytes and decoded values. It cannot recover invalid UTF-8 already replaced by the JSON encoder. */
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

func isJsonC1RuneAt(document []byte, index int) bool {
    if c1LeadByte != document[index] || index+1 >= len(document) {
        return false
    }

    return 0x80 <= document[index+1] && 0x9f >= document[index+1]
}
