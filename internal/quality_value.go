package internal

/* ParseQualityValue validates value against the qvalue grammar of RFC 7231 — a zero with up to three decimal digits, or a one with up to three zero decimals — and reports anything outside it as invalid instead of guessing a weight for it. It is the one reader for every q parameter the framework negotiates on: a bare float parse accepts NaN, infinities and out-of-range numbers, so the same malformed header could open, close or silently poison a negotiation depending on which reader saw it. */
func ParseQualityValue(value string) (float64, bool) {
    if "" == value {
        return 0, false
    }

    integerPart := value[0]
    if '0' != integerPart && '1' != integerPart {
        return 0, false
    }

    decimals := value[1:]
    if "" != decimals {
        if '.' != decimals[0] {
            return 0, false
        }

        decimals = decimals[1:]
        if 3 < len(decimals) {
            return 0, false
        }
    }

    qualityValue := 0.0
    if '1' == integerPart {
        qualityValue = 1.0
    }

    scale := 0.1
    for index := 0; index < len(decimals); index++ {
        digit := decimals[index]
        if digit < '0' || digit > '9' {
            return 0, false
        }

        if '1' == integerPart && '0' != digit {
            return 0, false
        }

        qualityValue += float64(digit-'0') * scale
        scale = scale / 10
    }

    return qualityValue, true
}
