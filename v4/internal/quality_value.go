package internal

/* ParseQualityValue validates a q parameter against the qvalue grammar of RFC 7231 and reports anything outside it as invalid. Digits past the third are accepted when they are zeros, since they cannot change the weight, and refused otherwise. It is the one reader for every q parameter the framework negotiates on. */
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
