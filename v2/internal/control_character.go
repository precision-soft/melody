package internal

import (
    "fmt"
    "strings"
)

/* EscapeControlCharacters replaces every C0 control character and DEL in the value with its visible escape spelling — the named ones as \n, \r, \t, the rest as \xNN — so text of unknown origin can be written to a terminal or a line-oriented log without repainting the one or forging records in the other. An embedded escape sequence stays data: the ESC byte that would start it is rendered as \x1b, and an embedded line break is rendered as \n instead of ending the record it belongs to. */
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

    for _, currentRune := range value {
        if false == isEscapedControlRune(currentRune, keepNewline) {
            builder.WriteRune(currentRune)

            continue
        }

        builder.WriteString(controlRuneSpelling(currentRune))
    }

    return builder.String()
}

func containsControlCharacter(value string, keepNewline bool) bool {
    for _, currentRune := range value {
        if true == isEscapedControlRune(currentRune, keepNewline) {
            return true
        }
    }

    return false
}

func isEscapedControlRune(currentRune rune, keepNewline bool) bool {
    if '\n' == currentRune {
        return false == keepNewline
    }

    return (0x20 > currentRune && 0 <= currentRune) || 0x7f == currentRune
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

    return fmt.Sprintf(`\x%02x`, currentRune)
}
