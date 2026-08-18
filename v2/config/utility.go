package config

import (
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

/* IntWithDefault answers the default only for a parameter that is absent: a parameter that exists but does not parse panics instead of silently becoming the default, because a mistyped value that quietly turns into a number nobody wrote is a misconfiguration running in disguise. */
func IntWithDefault(configParameter configcontract.Parameter, defaultValue int) int {
    if true == internal.IsNilInterface(configParameter) {
        return defaultValue
    }

    value, err := configParameter.Int()
    if nil != err {
        exception.Panic(
            exception.NewError(
                "parameter exists but does not parse as int; remove it to fall back to the default",
                nil,
                err,
            ),
        )
    }

    return value
}
