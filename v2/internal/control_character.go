package internal

import (
    "fmt"
    "strings"
)

/* EscapeControlCharacters replaces every control character in the value with its visible escape spelling — the named C0 ones as \n, \r, \t, every other C0 one, DEL and the C1 block as \xNN, and the two Unicode line separators as \uNNNN — so text of unknown origin can be written to a terminal or a line-oriented log without repainting the one or forging records in the other. An embedded escape sequence stays data whichever spelling it uses: the ESC byte that would start it is rendered as \x1b and the single-byte C1 introducer that starts one without ESC is rendered as \x9b. Every rune a reader splitting on Unicode line boundaries counts as a record end — the C0 breaks, NEL and the LINE and PARAGRAPH SEPARATOR — is rendered visibly instead of ending the record it belongs to. */
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
