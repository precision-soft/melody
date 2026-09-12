package internal

import (
    "unicode"
)

/* wideRuneRangeTable holds the principal East Asian Wide and Fullwidth blocks — CJK, Hangul, Kana, the fullwidth forms, and the emoji planes terminals render at two cells. It is deliberately the principal blocks and not the full Unicode East-Asian-Width database: ambiguous-width characters count as narrow, which is the convention terminals outside legacy CJK locales follow, and the blocks below cover what a table cell realistically carries. */
var wideRuneRangeTable = &unicode.RangeTable{
    R16: []unicode.Range16{
        {Lo: 0x1100, Hi: 0x115F, Stride: 1},
        {Lo: 0x2329, Hi: 0x232A, Stride: 1},
        {Lo: 0x2E80, Hi: 0x303E, Stride: 1},
        {Lo: 0x3041, Hi: 0x33FF, Stride: 1},
        {Lo: 0x3400, Hi: 0x4DBF, Stride: 1},
        {Lo: 0x4E00, Hi: 0x9FFF, Stride: 1},
        {Lo: 0xA000, Hi: 0xA4CF, Stride: 1},
        {Lo: 0xA960, Hi: 0xA97F, Stride: 1},
        {Lo: 0xAC00, Hi: 0xD7A3, Stride: 1},
        {Lo: 0xF900, Hi: 0xFAFF, Stride: 1},
        {Lo: 0xFE10, Hi: 0xFE19, Stride: 1},
        {Lo: 0xFE30, Hi: 0xFE52, Stride: 1},
        {Lo: 0xFE54, Hi: 0xFE66, Stride: 1},
        {Lo: 0xFE68, Hi: 0xFE6B, Stride: 1},
        {Lo: 0xFF00, Hi: 0xFF60, Stride: 1},
        {Lo: 0xFFE0, Hi: 0xFFE6, Stride: 1},
    },
    R32: []unicode.Range32{
        {Lo: 0x16FE0, Hi: 0x16FE4, Stride: 1},
        {Lo: 0x17000, Hi: 0x187F7, Stride: 1},
        {Lo: 0x18800, Hi: 0x18CD5, Stride: 1},
        {Lo: 0x1B000, Hi: 0x1B2FF, Stride: 1},
        {Lo: 0x1F004, Hi: 0x1F004, Stride: 1},
        {Lo: 0x1F0CF, Hi: 0x1F0CF, Stride: 1},
        {Lo: 0x1F18E, Hi: 0x1F18E, Stride: 1},
        {Lo: 0x1F191, Hi: 0x1F19A, Stride: 1},
        {Lo: 0x1F200, Hi: 0x1F2FF, Stride: 1},
        {Lo: 0x1F300, Hi: 0x1F64F, Stride: 1},
        {Lo: 0x1F680, Hi: 0x1F6FF, Stride: 1},
        {Lo: 0x1F900, Hi: 0x1F9FF, Stride: 1},
        {Lo: 0x1FA70, Hi: 0x1FAFF, Stride: 1},
        {Lo: 0x20000, Hi: 0x2FFFD, Stride: 1},
        {Lo: 0x30000, Hi: 0x3FFFD, Stride: 1},
    },
}

/* zeroWidthRuneRangeTable holds what occupies no cell of its own: the Hangul jungseong and jongseong jamo, which compose into the syllable that precedes them. The combining marks and the format characters are matched by category below rather than listed here. */
var zeroWidthRuneRangeTable = &unicode.RangeTable{
    R16: []unicode.Range16{
        {Lo: 0x1160, Hi: 0x11FF, Stride: 1},
        {Lo: 0xD7B0, Hi: 0xD7FF, Stride: 1},
    },
}

/* RuneDisplayWidth answers how many terminal cells the rune occupies: zero for a combining mark, an enclosing mark, a format character — the zero-width joiners and the variation selectors live in those categories — and the trailing Hangul jamo; two for the principal East Asian Wide and Fullwidth blocks; one for everything else. A control character answers one, because every renderer here escapes controls before measuring. */
func RuneDisplayWidth(value rune) int {
    if true == unicode.In(value, unicode.Mn, unicode.Me, unicode.Cf) {
        return 0
    }

    if true == unicode.Is(zeroWidthRuneRangeTable, value) {
        return 0
    }

    if true == unicode.Is(wideRuneRangeTable, value) {
        return 2
    }

    return 1
}

/* DisplayWidth answers the terminal cells the string occupies — the measure a table's column arithmetic needs, where a rune count reads a CJK ideogram as one cell and renders a column two cells short of where it measured. */
func DisplayWidth(value string) int {
    width := 0
    for _, runeValue := range value {
        width = width + RuneDisplayWidth(runeValue)
    }

    return width
}
