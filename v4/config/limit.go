package config

import (
    "fmt"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const maximumUnavailableRetryAfter = time.Hour

/* ResolveUnavailableRetryAfter decides the delay against UnavailableRetryAfterRule and refuses a value that is not a whole number of seconds, naming field. */
func ResolveUnavailableRetryAfter(limit bound.Duration, field string) (time.Duration, error) {
    rule := UnavailableRetryAfterRule()
    rule.Name = field

    resolved, resolveErr := bound.Resolve(limit, rule)
    if nil != resolveErr {
        return 0, resolveErr
    }

    if 0 != resolved.Value%time.Second {
        return 0, exception.NewError(
            "the limit is not a whole number of seconds, and Retry-After is served in whole seconds",
            exceptioncontract.Context{
                "field": field,
                "value": resolved.Value.String(),
            },
            nil,
        )
    }

    return resolved.Value, nil
}

func parameterLimit[T bound.Unit](parameter configcontract.Parameter, environmentKey string) (bound.Limit[T], error) {
    text := ""
    if value := parameter.Value(); nil != value {
        if stringValue, isString := value.(string); true == isString {
            text = stringValue
        } else {
            text = fmt.Sprint(value)
        }
    }

    limit := bound.Limit[T]{}
    if unmarshalErr := limit.UnmarshalText([]byte(text)); nil != unmarshalErr {
        return bound.Limit[T]{}, exception.NewError(
            "invalid environment value",
            limitRefusalContext(parameter, environmentKey),
            limitRefusalCause(parameter, unmarshalErr),
        )
    }

    return limit, nil
}

func resolveParameter[T bound.Unit](parameter configcontract.Parameter, environmentKey string, rule bound.Rule[T]) (bound.Resolved[T], error) {
    limit, limitErr := parameterLimit[T](parameter, environmentKey)
    if nil != limitErr {
        return bound.Resolved[T]{}, limitErr
    }

    resolved, resolveErr := bound.Resolve(limit, rule)
    if nil != resolveErr {
        return bound.Resolved[T]{}, exception.NewError(
            "invalid environment value",
            limitRefusalContext(parameter, environmentKey),
            limitRefusalCause(parameter, resolveErr),
        )
    }

    return resolved, nil
}

func checkedParameterLimit[T bound.Unit](parameter configcontract.Parameter, environmentKey string, rule bound.Rule[T]) (bound.Limit[T], error) {
    limit, limitErr := parameterLimit[T](parameter, environmentKey)
    if nil != limitErr {
        return bound.Limit[T]{}, limitErr
    }

    if true == limit.IsAbsent() {
        return limit, nil
    }

    if _, resolveErr := bound.Resolve(limit, rule); nil != resolveErr {
        return bound.Limit[T]{}, exception.NewError(
            "invalid environment value",
            limitRefusalContext(parameter, environmentKey),
            limitRefusalCause(parameter, resolveErr),
        )
    }

    return limit, nil
}

func checkedUnavailableRetryAfter(parameter configcontract.Parameter) (bound.Duration, error) {
    limit, limitErr := checkedParameterLimit(parameter, HttpUnavailableRetryAfterKey, UnavailableRetryAfterRule())
    if nil != limitErr || true == limit.IsAbsent() {
        return limit, limitErr
    }

    if _, resolveErr := ResolveUnavailableRetryAfter(limit, UnavailableRetryAfterRule().Name); nil != resolveErr {
        return bound.Duration{}, exception.NewError(
            "invalid environment value",
            limitRefusalContext(parameter, HttpUnavailableRetryAfterKey),
            limitRefusalCause(parameter, resolveErr),
        )
    }

    return limit, nil
}

func checkedParameterSwitch(parameter configcontract.Parameter, environmentKey string) (configcontract.Switch, error) {
    value := parameter.Value()
    if text, isString := value.(string); true == isString && "" == strings.TrimSpace(text) {
        return configcontract.Switch{}, nil
    }

    switchValue, isSet, parseErr := internal.Bool(value, environmentKey)
    if nil != parseErr {
        return configcontract.Switch{}, exception.NewError(
            "invalid environment value",
            limitRefusalContext(parameter, environmentKey),
            limitRefusalCause(parameter, parseErr),
        )
    }

    return configcontract.Switch{Value: switchValue, IsSet: isSet}, nil
}

/* checkedSessionCookieSecure admits always alone: the key wins over the Go field, so from-scheme or never would let a deployment lower an always the composition root chose; only the Go field lowers the policy */
func checkedSessionCookieSecure(parameter configcontract.Parameter) (string, error) {
    value := parameter.Value()
    text, isString := value.(string)
    if true == isString && "" == strings.TrimSpace(text) {
        return "", nil
    }

    if true == isString && HttpSessionCookieSecureAlways == text {
        return text, nil
    }

    refusalContext := limitRefusalContext(parameter, HttpSessionCookieSecureKey)
    refusalContext["admitted"] = []string{HttpSessionCookieSecureAlways}
    if false == parameter.IsSecret() {
        refusalContext["value"] = value
    }

    return "", exception.NewError("invalid environment value", refusalContext, nil)
}

/* an empty value is refused, not read as the default: the key has a default, so an empty line is a typo, and a second spelling of the default would be a second source */
func checkedParameterJournalMode(parameter configcontract.Parameter, environmentKey string, floor os.FileMode, refusal string) (os.FileMode, error) {
    value := parameter.Value()

    text, isString := value.(string)
    if true == isString {
        text = strings.TrimPrefix(strings.TrimSpace(text), "0o")

        if "" != text {
            mode, parseErr := strconv.ParseUint(text, 8, 32)
            if nil == parseErr && true == internal.IsJournalMode(os.FileMode(mode), floor) {
                return os.FileMode(mode), nil
            }
        }
    }

    refusalContext := limitRefusalContext(parameter, environmentKey)
    if false == parameter.IsSecret() {
        refusalContext["value"] = value
    }

    return 0, exception.NewError(refusal, refusalContext, nil)
}

/* an overridden parameter does not read its key, so the refusal names the parameter the override wrote */
func limitRefusalContext(parameter configcontract.Parameter, environmentKey string) exceptioncontract.Context {
    context := exceptioncontract.Context{
        "environmentKey": environmentKey,
    }

    if configParameter, isConfigParameter := parameter.(*Parameter); true == isConfigParameter && true == configParameter.overridden {
        context["parameterName"] = configParameter.name
        context["overridden"] = true
    }

    return context
}

/* the cause quotes the value, and an override may feed a limit from a secret key */
func limitRefusalCause(parameter configcontract.Parameter, causeErr error) error {
    if true == parameter.IsSecret() {
        return nil
    }

    return causeErr
}

func MaxRequestBodyBytesRule() bound.Rule[int] {
    return bound.Rule[int]{
        Name:    "HttpConfiguration.MaxRequestBodyBytes",
        Default: DefaultHttpMaxRequestBodyBytes,
        Minimum: 1,
    }
}

func StaticCacheMaxAgeRule() bound.Rule[int] {
    return bound.Rule[int]{
        Name:    "HttpConfiguration.StaticCacheMaxAge",
        Default: DefaultStaticCacheMaxAge,
        Minimum: 0,
    }
}

func SessionTtlRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              "HttpConfiguration.SessionTtl",
        Default:           DefaultSessionTtl,
        Minimum:           bound.TtlMinimum,
        UnlimitedAdmitted: true,
    }
}

func SessionTombstoneRetentionRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "HttpConfiguration.SessionTombstoneRetention",
        Default: DefaultSessionTombstoneRetention,
        Minimum: bound.TtlMinimum,
    }
}

func SessionStorageTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              "HttpConfiguration.SessionStorageTimeout",
        Default:           DefaultSessionStorageTimeout,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }
}

func SessionMaxSessionsRule() bound.Rule[int] {
    return bound.Rule[int]{
        Name:              "HttpConfiguration.SessionMaxSessions",
        Default:           DefaultSessionMaxSessions,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }
}

func ShutdownTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpServer.ShutdownTimeout",
        Default: DefaultHttpShutdownTimeout,
        Minimum: bound.TimeoutMinimum,
    }
}

/* HttpReadTimeoutRule and the three http timeout rules after it refuse unlimited: net/http reads no bound as no timeout. */
func HttpReadTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpServer.ReadTimeout",
        Default: DefaultHttpReadTimeout,
        Minimum: bound.TimeoutMinimum,
    }
}

func HttpReadHeaderTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpServer.ReadHeaderTimeout",
        Default: DefaultHttpReadHeaderTimeout,
        Minimum: bound.TimeoutMinimum,
    }
}

func HttpWriteTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpServer.WriteTimeout",
        Default: DefaultHttpWriteTimeout,
        Minimum: bound.TimeoutMinimum,
    }
}

func HttpIdleTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpServer.IdleTimeout",
        Default: DefaultHttpIdleTimeout,
        Minimum: bound.TimeoutMinimum,
    }
}

func HttpMaxHeaderBytesRule() bound.Rule[int] {
    return bound.Rule[int]{
        Name:    "ApplicationConfig.HttpServer.MaxHeaderBytes",
        Default: DefaultHttpMaxHeaderBytes,
        Minimum: 4 << 10,
    }
}

func TeardownTimeoutRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              "ApplicationConfig.TeardownTimeout",
        Default:           DefaultTeardownTimeout,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }
}

/* UnavailableRetryAfterRule bounds the Retry-After of an infrastructure-failure 503: 5s when left out, 1s to 1h, unlimited refused. */
func UnavailableRetryAfterRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.HttpKernel.UnavailableRetryAfter",
        Default: DefaultUnavailableRetryAfter,
        Minimum: bound.TtlMinimum,
        Maximum: maximumUnavailableRetryAfter,
    }
}
