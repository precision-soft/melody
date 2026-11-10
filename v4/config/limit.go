package config

import (
    "fmt"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

var (
    MaxRequestBodyBytesRule = bound.Rule[int]{
        Name:    "HttpConfiguration.MaxRequestBodyBytes",
        Default: DefaultHttpMaxRequestBodyBytes,
        Minimum: 1,
    }

    StaticCacheMaxAgeRule = bound.Rule[int]{
        Name:    "HttpConfiguration.StaticCacheMaxAge",
        Default: DefaultStaticCacheMaxAge,
        Minimum: 0,
    }

    SessionTtlRule = bound.Rule[time.Duration]{
        Name:              "HttpConfiguration.SessionTtl",
        Default:           DefaultSessionTtl,
        Minimum:           bound.TtlMinimum,
        UnlimitedAdmitted: true,
    }

    SessionTombstoneRetentionRule = bound.Rule[time.Duration]{
        Name:    "HttpConfiguration.SessionTombstoneRetention",
        Default: DefaultSessionTombstoneRetention,
        Minimum: bound.TtlMinimum,
    }

    SessionMaxSessionsRule = bound.Rule[int]{
        Name:              "HttpConfiguration.SessionMaxSessions",
        Default:           DefaultSessionMaxSessions,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }

    ShutdownTimeoutRule = bound.Rule[time.Duration]{
        Name:    "HttpConfiguration.ShutdownTimeout",
        Default: DefaultHttpShutdownTimeout,
        Minimum: bound.TimeoutMinimum,
    }
)

/* unlimited is refused on the five server limits, where net/http reads no bound as no timeout */
var (
    HttpReadTimeoutRule = bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.ReadTimeout",
        Default: DefaultHttpReadTimeout,
        Minimum: bound.TimeoutMinimum,
    }

    HttpReadHeaderTimeoutRule = bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.ReadHeaderTimeout",
        Default: DefaultHttpReadHeaderTimeout,
        Minimum: bound.TimeoutMinimum,
    }

    HttpWriteTimeoutRule = bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.WriteTimeout",
        Default: DefaultHttpWriteTimeout,
        Minimum: bound.TimeoutMinimum,
    }

    HttpIdleTimeoutRule = bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.IdleTimeout",
        Default: DefaultHttpIdleTimeout,
        Minimum: bound.TimeoutMinimum,
    }

    HttpMaxHeaderBytesRule = bound.Rule[int]{
        Name:    "ApplicationConfig.MaxHeaderBytes",
        Default: DefaultHttpMaxHeaderBytes,
        Minimum: 4 << 10,
    }

    TeardownTimeoutRule = bound.Rule[time.Duration]{
        Name:              "ApplicationConfig.TeardownTimeout",
        Default:           DefaultTeardownTimeout,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }

    /* UnavailableRetryAfterRule bounds the Retry-After of a 503 answering an infrastructure failure: 5s when left out, 1s to 1h, unlimited refused; ResolveUnavailableRetryAfter adds the whole-second check. */
    UnavailableRetryAfterRule = bound.Rule[time.Duration]{
        Name:    "ApplicationConfig.UnavailableRetryAfter",
        Default: DefaultUnavailableRetryAfter,
        Minimum: bound.TtlMinimum,
        Maximum: maximumUnavailableRetryAfter,
    }
)

const maximumUnavailableRetryAfter = time.Hour

/* ResolveUnavailableRetryAfter decides the Retry-After delay of a 503 answering an infrastructure failure against UnavailableRetryAfterRule, and refuses a value that is not a whole number of seconds, since the header carries whole seconds; every refusal names field. */
func ResolveUnavailableRetryAfter(limit bound.Duration, field string) (time.Duration, error) {
    rule := UnavailableRetryAfterRule
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
            exceptioncontract.Context{
                "environmentKey": environmentKey,
            },
            unmarshalErr,
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
            exceptioncontract.Context{
                "environmentKey": environmentKey,
            },
            resolveErr,
        )
    }

    return resolved, nil
}

/* an absent limit stays absent, for the application's own default to fill */
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
            exceptioncontract.Context{
                "environmentKey": environmentKey,
            },
            resolveErr,
        )
    }

    return limit, nil
}

/* checkedUnavailableRetryAfter reads MELODY_HTTP_UNAVAILABLE_RETRY_AFTER as checkedParameterLimit reads a limit, the whole-second check included */
func checkedUnavailableRetryAfter(parameter configcontract.Parameter) (bound.Duration, error) {
    limit, limitErr := checkedParameterLimit(parameter, HttpUnavailableRetryAfterKey, UnavailableRetryAfterRule)
    if nil != limitErr || true == limit.IsAbsent() {
        return limit, limitErr
    }

    if _, resolveErr := ResolveUnavailableRetryAfter(limit, UnavailableRetryAfterRule.Name); nil != resolveErr {
        return bound.Duration{}, exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": HttpUnavailableRetryAfterKey,
            },
            resolveErr,
        )
    }

    return limit, nil
}

/* checkedParameterSwitch reads a boolean key the way checkedParameterLimit reads a limit: an empty value is unset, for the application's own config to decide, and a value that is not a boolean is refused naming the key. */
func checkedParameterSwitch(parameter configcontract.Parameter, environmentKey string) (configcontract.Switch, error) {
    value := parameter.Value()
    if text, isString := value.(string); true == isString && "" == strings.TrimSpace(text) {
        return configcontract.Switch{}, nil
    }

    switchValue, isSet, parseErr := internal.Bool(value, environmentKey)
    if nil != parseErr {
        return configcontract.Switch{}, exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": environmentKey,
            },
            parseErr,
        )
    }

    return configcontract.Switch{Value: switchValue, IsSet: isSet}, nil
}
