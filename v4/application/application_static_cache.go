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

    warningContext := loggingcontract.Context{"environmentKeys": []string{config.StaticCacheDisabledKey, config.StaticCacheMaxAgeKey}}
    if true == maxAgeParameter.IsOverridden() {
        /* an override does not read the key, so the warning names the parameter the override wrote */
        warningContext = loggingcontract.Context{"environmentKeys": []string{config.StaticCacheDisabledKey}, "parameterName": config.KernelStaticCacheMaxAge, "overridden": true}
    }

    instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
        Name:    internal.BootWarningNameStaticCacheMaxAgeIgnored,
        Message: "the static cache max age is set beside the disabled static cache and is ignored: the file server sends no cache header and no validator",
        Context: warningContext,
    })
}
