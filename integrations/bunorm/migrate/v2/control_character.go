package migrate

import (
    "fmt"
    "strings"
    "unicode/utf8"
)

/* escapeControlCharacters replaces every control character in the value with its visible escape spelling — the named C0 ones as \n, \r, \t, every other C0 one, DEL and the C1 block as \xNN, and the two Unicode line separators as \uNNNN — so text of server origin (an error message off the wire, a reported version string, a failed statement) can be written to the operator's terminal without repainting it or forging lines in a captured log. The C1 block earns its place twice: a terminal decoding UTF-8 obeys those runes the way it obeys the ESC sequences they abbreviate, and NEL at U+0085 ends a record for every reader that splits on Unicode line boundaries — the two separators close the rest of that set. A byte that is not part of a valid UTF-8 sequence is rendered as \xNN of that byte, whatever its value: the server can answer the single-byte C1 introducer 0x9b raw, and a walk over runes decoded that byte as U+FFFD, let it through as sent, and, once any other control character forced the rewrite, replaced it with U+FFFD in silence; the result is therefore always valid UTF-8, and a genuine U+FFFD in the text, three valid bytes, passes through as itself. keepNewline is the multi-line form the query rendering uses, where a real line break is the point and every other control character is still escaped. Mirrors the framework's internal helper, which a separate module cannot import; the json rendering is the framework printer's, which escapes the C1 block itself and writes a byte that is not valid UTF-8 as U+FFFD, the encoder's documented answer. */
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
