package internal

/* ParseQualityValue parses HTTP quality values in [0,1] with at most three significant decimal places. Additional trailing zeros are accepted; extra nonzero digits, NaN and infinities are refused. */
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
