package application

import (
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* the disabled static cache wins over a max age set beside it, so a layered .env can switch the cache off over a base that sets the max age; only an http process serves files, so only it warns */
func (instance *Application) collectIgnoredStaticCacheMaxAge() {
    if config.ModeHttp != instance.runtimeFlags.Mode() || nil == instance.configuration || true == internal.IsNilInterface(instance.configuration.Http()) {
        return
    }

    if false == instance.configuration.Http().StaticCacheDisabled() {
        return
    }

    maxAgeParameter := instance.configuration.Get(config.KernelStaticCacheMaxAge)
    if true == internal.IsNilInterface(maxAgeParameter) || true == maxAgeParameter.IsDefault() {
        return
    }

    /* each value is named where it came from: the key of a .env line, the parameter of an override, which reads no key */
    environmentKeys := make([]string, 0, 2)
    parameterNames := make([]string, 0, 2)
    for _, source := range []struct {
        parameterName  string
        environmentKey string
    }{
        {config.KernelStaticCacheDisabled, config.StaticCacheDisabledKey},
        {config.KernelStaticCacheMaxAge, config.StaticCacheMaxAgeKey},
    } {
        parameter := instance.configuration.Get(source.parameterName)
        if false == internal.IsNilInterface(parameter) && true == parameter.IsOverridden() {
            parameterNames = append(parameterNames, source.parameterName)

            continue
        }

        environmentKeys = append(environmentKeys, source.environmentKey)
    }

    warningContext := loggingcontract.Context{}
    if 0 < len(environmentKeys) {
        warningContext["environmentKeys"] = environmentKeys
    }

    if 0 < len(parameterNames) {
        warningContext["parameterNames"] = parameterNames
        warningContext["overridden"] = true
    }

    instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
        Name:    internal.BootWarningNameStaticCacheMaxAgeIgnored,
        Message: "the static cache max age is set beside the disabled static cache and is ignored: the file server sends no cache header and no validator",
        Context: warningContext,
    })
}
