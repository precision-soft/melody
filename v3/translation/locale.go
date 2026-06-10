package translation

import (
    "math"
    "strings"
)

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
