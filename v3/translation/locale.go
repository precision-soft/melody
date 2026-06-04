package translation

func baseLocale(locale string) string {
    for index := 0; index < len(locale); index++ {
        if '_' == locale[index] || '-' == locale[index] {
            return locale[:index]
        }
    }

    return locale
}

func pluralCategory(locale string, number float64) string {
    if 1 == number {
        return "one"
    }

    return "other"
}
