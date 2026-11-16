package translation

import (
    "math"
    "strings"
)

/* canonicalLocale writes a locale in BCP 47 case, RFC 5646 2.1.1: the language lower, a four-letter script title, a two-letter region upper, every other subtag lower, and every subtag after a singleton (an extension or x- private use) lower, so two spellings of one tag find one catalog. Only ASCII letters are folded, every other byte kept as written; the separators are kept too, an underscore not folded to a hyphen. */
func canonicalLocale(locale string) string {
    var builder strings.Builder
    builder.Grow(len(locale))

    subtagIndex := 0
    afterSingleton := false
    start := 0
    for index := 0; index <= len(locale); index++ {
        if index < len(locale) && '_' != locale[index] && '-' != locale[index] {
            continue
        }

        subtag := locale[start:index]
        writeCanonicalSubtag(&builder, subtag, 0 == subtagIndex || true == afterSingleton)
        if 1 == len(subtag) {
            afterSingleton = true
        }

        if index < len(locale) {
            builder.WriteByte(locale[index])
        }

        subtagIndex++
        start = index + 1
    }

    return builder.String()
}

func writeCanonicalSubtag(builder *strings.Builder, subtag string, lower bool) {
    titled := false == lower && 4 == len(subtag) && true == isAsciiLetters(subtag)
    upper := false == lower && 2 == len(subtag) && true == isAsciiLetters(subtag)

    for index := 0; index < len(subtag); index++ {
        character := subtag[index]
        if true == upper || (true == titled && 0 == index) {
            builder.WriteByte(asciiUpper(character))
            continue
        }

        builder.WriteByte(asciiLower(character))
    }
}

func asciiLower(character byte) byte {
    if character >= 'A' && character <= 'Z' {
        return character + ('a' - 'A')
    }

    return character
}

func asciiUpper(character byte) byte {
    if character >= 'a' && character <= 'z' {
        return character - ('a' - 'A')
    }

    return character
}

func isAsciiLetters(value string) bool {
    for index := 0; index < len(value); index++ {
        character := value[index] | 0x20
        if character < 'a' || character > 'z' {
            return false
        }
    }

    return true
}

func baseLocale(locale string) string {
    for index := 0; index < len(locale); index++ {
        if '_' == locale[index] || '-' == locale[index] {
            return locale[:index]
        }
    }

    return locale
}

func pluralCategory(locale string, number float64) string {
    n := math.Abs(number)
    i := math.Trunc(n)
    hasFraction := n != i

    switch strings.ToLower(baseLocale(locale)) {
    case "ro", "mo":
        return romanianPluralCategory(n, i, hasFraction)
    case "ru":
        return russianPluralCategory(i, hasFraction)
    default:
        if false == hasFraction && 1 == i {
            return "one"
        }

        return "other"
    }
}

func romanianPluralCategory(n float64, i float64, hasFraction bool) string {
    if false == hasFraction && 1 == i {
        return "one"
    }

    mod100 := math.Mod(i, 100)
    if true == hasFraction || 0 == n || (mod100 >= 1 && mod100 <= 19) {
        return "few"
    }

    return "other"
}

func russianPluralCategory(i float64, hasFraction bool) string {
    if true == hasFraction {
        return "other"
    }

    mod10 := math.Mod(i, 10)
    mod100 := math.Mod(i, 100)

    if 1 == mod10 && 11 != mod100 {
        return "one"
    }

    if mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) {
        return "few"
    }

    return "many"
}
