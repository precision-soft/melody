package container

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ClosedWithScope hands the installed value to the scope's teardown, for a caller that built it for this scope alone; by default an override belongs to whoever installed it. */
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
