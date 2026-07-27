package container

import (
    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

/* ClosedWithScope hands the installed value to the scope's teardown, alongside the services the scope built for itself. The default is the opposite: an override is installed from outside and belongs to whoever installed it, because the installer usually still needs it after the scope is gone. This option is for the caller that built the value for this one scope and holds no other reference to it. */
func ClosedWithScope() containercontract.OverrideOption {
    return func(option *containercontract.OverrideOptions) {
        option.ClosedWithScope = true
    }
}

func buildOverrideOption() *containercontract.OverrideOptions {
    return &containercontract.OverrideOptions{
        ClosedWithScope: false,
    }
}

func applyOverrideOptions(options []containercontract.OverrideOption) *containercontract.OverrideOptions {
    merged := buildOverrideOption()
    for _, optionFunc := range options {
        if nil == optionFunc {
            continue
        }

        optionFunc(merged)
    }

    return merged
}
