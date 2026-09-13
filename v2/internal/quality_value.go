package internal

/* ParseQualityValue validates value against the qvalue grammar of RFC 7231 — a zero with up to three decimal digits, or a one with up to three zero decimals — and reports anything outside it as invalid instead of guessing a weight for it. It is the one reader for every q parameter the framework negotiates on: a bare float parse accepts NaN, infinities and out-of-range numbers, so the same malformed header could open, close or silently poison a negotiation depending on which reader saw it.

   Digits past the third are read when they are zeros and refused otherwise. Excess trailing zeros are precision the grammar cannot carry and the value cannot change — q=1.0000 says exactly what q=1.0 says — and clients do write them; refusing them dropped the member from the negotiation entirely, so an api client that asked for application/json;q=1.0000 was served the html error page instead. A non-zero digit there does carry a weight the grammar cannot express, and stays refused. */
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
            for index := 3; index < len(decimals); index++ {
                if '0' != decimals[index] {
                    return 0, false
                }
            }

            decimals = decimals[:3]
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
