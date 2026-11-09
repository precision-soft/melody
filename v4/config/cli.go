package config

import (
    "strings"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

func newCliConfiguration(name string, description string, bootWarningsIgnored []string, bootWarningsDisabled bool) (*cliConfiguration, error) {
    cliConfigurationInstance := &cliConfiguration{
        name:                 strings.TrimSpace(name),
        description:          strings.TrimSpace(description),
        bootWarningsIgnored:  append([]string{}, bootWarningsIgnored...),
        bootWarningsDisabled: bootWarningsDisabled,
    }

    validateErr := cliConfigurationInstance.validate()
    if nil != validateErr {
        return nil, validateErr
    }

    return cliConfigurationInstance, nil
}

type cliConfiguration struct {
    name                 string
    description          string
    bootWarningsIgnored  []string
    bootWarningsDisabled bool
}

func (instance *cliConfiguration) Name() string {
    return instance.name
}

func (instance *cliConfiguration) Description() string {
    return instance.description
}

func (instance *cliConfiguration) BootWarningsIgnored() []string {
    return append([]string{}, instance.bootWarningsIgnored...)
}

func (instance *cliConfiguration) BootWarningsDisabled() bool {
    return instance.bootWarningsDisabled
}

func (instance *cliConfiguration) validate() error {
    if "" == instance.name {
        return exception.NewError(
            "cli name may not be empty",
            nil,
            nil,
        )
    }

    if true == instance.bootWarningsDisabled && 0 < len(instance.bootWarningsIgnored) {
        return exception.NewError(
            "cli boot warnings are both disabled and ignored by code: a disabled journal writes no boot warning, so the ignored codes are read by nothing; keep one of the two keys",
            exceptioncontract.Context{
                "environmentKeys": []string{CliBootWarningsIgnoredKey, CliBootWarningsDisabledKey},
            },
            nil,
        )
    }

    listed := make(map[string]struct{}, len(instance.bootWarningsIgnored))
    for index, code := range instance.bootWarningsIgnored {
        if refusal := refuseIgnoredBootWarningCode(index, code, listed); nil != refusal {
            return refusal
        }

        listed[code] = struct{}{}
    }

    return nil
}

/* each refused code would silence nothing while the operator believes it does */
func refuseIgnoredBootWarningCode(index int, code string, listed map[string]struct{}) error {
    context := exceptioncontract.Context{
        "environmentKey": CliBootWarningsIgnoredKey,
        "index":          index,
        "code":           code,
    }

    if "" == code {
        return exception.NewError("cli boot warning code may not be empty", context, nil)
    }

    if _, isListed := listed[code]; true == isListed {
        return exception.NewError("cli boot warning code is listed twice", context, nil)
    }

    if false == internal.IsKnownBootWarningName(code) {
        context["cliCodes"] = internal.CliBootWarningNames()

        return exception.NewError("cli boot warning code names no boot warning the framework writes", context, nil)
    }

    if false == internal.IsCliBootWarningName(code) {
        context["cliCodes"] = internal.CliBootWarningNames()

        return exception.NewError("cli boot warning code names a boot warning only an http process writes, so a cli process never reads it", context, nil)
    }

    return nil
}

var _ configcontract.CliConfiguration = (*cliConfiguration)(nil)
