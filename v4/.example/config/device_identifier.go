package config

import (
    "github.com/precision-soft/melody/v4/.example/handler/accesstoken"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyexception "github.com/precision-soft/melody/v4/exception"
)

const defaultDeviceIdentifierMaximumBytes = 64

var deviceIdentifierMaximumBytesRule = melodybound.Rule[int]{
    Default: defaultDeviceIdentifierMaximumBytes,
    Minimum: 1,
    Maximum: accesstoken.DeviceIdentifierMaximumBytesCeiling,
}

/* deviceIdentifierMaximumBytes is the bound the access-token doors hold a device identifier to; a value outside 1 to 255 refuses the boot rather than leaving the bound to a typo */
func (instance *Module) deviceIdentifierMaximumBytes() int {
    resolved, resolveErr := melodyconfig.ResolveLimitParameter(
        instance.configuration.MustGet(parameterDeviceIdentifierMaxBytes),
        environmentKeyDeviceIdentifierMaxBytes,
        deviceIdentifierMaximumBytesRule,
    )
    if nil != resolveErr {
        melodyexception.Panic(melodyexception.FromError(resolveErr))
    }

    return resolved.Value
}
