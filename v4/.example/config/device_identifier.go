package config

import (
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v4/.example/handler/accesstoken"
    melodyexception "github.com/precision-soft/melody/v4/exception"
)

const defaultDeviceIdentifierMaximumBytes = 64

/* deviceIdentifierMaximumBytes is the bound the access-token doors hold a device identifier to; a value outside 1 to 255 refuses the boot rather than leaving the bound to a typo */
func (instance *Module) deviceIdentifierMaximumBytes() int {
    value := strings.TrimSpace(instance.environmentValue(environmentKeyDeviceIdentifierMaxBytes))
    if "" == value {
        return defaultDeviceIdentifierMaximumBytes
    }

    maximumBytes, parseErr := strconv.Atoi(value)
    if nil != parseErr || 1 > maximumBytes || accesstoken.DeviceIdentifierMaximumBytesCeiling < maximumBytes {
        melodyexception.Panic(melodyexception.NewError(
            "the device identifier bound is not an integer from 1 to 255",
            map[string]any{"key": environmentKeyDeviceIdentifierMaxBytes, "value": value},
            parseErr,
        ))
    }

    return maximumBytes
}
