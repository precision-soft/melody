package accesstoken

import (
    "fmt"
)

/* DeviceIdentifierMaximumBytesCeiling is the most a deployment may bound a device identifier to */
const DeviceIdentifierMaximumBytesCeiling = 255

/* the alphabet is fixed: it keeps the identifier out of the token store's key grammar, where only the length is the deployment's to choose */
func validDeviceIdentifier(value string, maximumBytes int) bool {
    if "" == value || maximumBytes < len(value) {
        return false
    }

    for index := 0; index < len(value); index++ {
        character := value[index]

        if ('a' <= character && 'z' >= character) || ('A' <= character && 'Z' >= character) || ('0' <= character && '9' >= character) {
            continue
        }

        if '.' == character || '_' == character || '-' == character {
            continue
        }

        return false
    }

    return true
}

func deviceIdentifierRefusal(maximumBytes int) string {
    return fmt.Sprintf("deviceIdentifier must be 1 to %d characters of letters, digits, dot, underscore or hyphen", maximumBytes)
}
