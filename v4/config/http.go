package config

import (
    "net"
    "regexp"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

var (
    defaultLocalePattern = regexp.MustCompile(`^[a-z]{2}(-[A-Za-z]{2})?$`)
)

/* DefaultSessionTtl is the session lifetime when MELODY_HTTP_SESSION_TTL says nothing; it is bounded, since every cookie-less request that writes a session mints an entry. */
const DefaultSessionTtl = 24 * time.Hour

/* DefaultHttpMaxRequestBodyBytes is the request body cap when MELODY_HTTP_MAX_REQUEST_BODY_BYTES says nothing, 1 MiB. */
const DefaultHttpMaxRequestBodyBytes = 1 << 20

/* DefaultStaticCacheMaxAge is the max-age in seconds the built-in file server sends when MELODY_STATIC_CACHE_MAX_AGE says nothing. */
const DefaultStaticCacheMaxAge = 3600

/* the per-request server limits when neither MELODY_HTTP_* nor ApplicationConfig names one */
const (
    DefaultHttpReadTimeout       = 15 * time.Second
    DefaultHttpReadHeaderTimeout = 5 * time.Second
    DefaultHttpWriteTimeout      = 30 * time.Second
    DefaultHttpIdleTimeout       = 60 * time.Second
    DefaultHttpMaxHeaderBytes    = 1 << 20
)

/* DefaultSessionTombstoneRetention is how long a deleted session id keeps refusing a write-back when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. */
const DefaultSessionTombstoneRetention = 5 * time.Minute

/* DefaultSessionStorageTimeout bounds every session storage call when MELODY_HTTP_SESSION_STORAGE_TIMEOUT says nothing. */
const DefaultSessionStorageTimeout = 5 * time.Second

/* DefaultSessionMaxSessions is how many sessions the framework-wired in-memory storage holds when MELODY_HTTP_SESSION_MAX_SESSIONS says nothing. */
const DefaultSessionMaxSessions = 100_000

/* DefaultUnavailableRetryAfter is the Retry-After of an infrastructure-failure 503 when neither MELODY_HTTP_UNAVAILABLE_RETRY_AFTER nor ApplicationConfig names one. */
const DefaultUnavailableRetryAfter = 5 * time.Second

/* DefaultHttpShutdownTimeout is how long a stopping http server waits for admitted requests when neither MELODY_HTTP_SHUTDOWN_TIMEOUT nor ApplicationConfig.HttpShutdownTimeout names one. */
const DefaultHttpShutdownTimeout = 5 * time.Second

func newHttpConfiguration(
    address string,
    publicUrl string,
    defaultLocale string,
    publicDirectory string,
    staticIndexFile string,
    maxRequestBodyBytes bound.Resolved[int],
    staticCacheDisabled bool,
    staticCacheMaxAge bound.Resolved[int],
    staticExcludedPaths []string,
    sessionTtl bound.Resolved[time.Duration],
    sessionTombstoneRetention bound.Resolved[time.Duration],
    sessionStorageTimeout bound.Resolved[time.Duration],
    sessionMaxSessions bound.Resolved[int],
    shutdownTimeout bound.Duration,
    serverLimits httpServerLimits,
) (*httpConfiguration, error) {
    if "" == strings.TrimSpace(address) {
        return nil, exception.NewError("http address may not be empty", nil, nil)
    }

    if false == strings.Contains(address, ":") {
        address = ":" + address
    }

    normalizedPublicUrl, publicUrlRefusal := internal.NormalizePublicUrl(publicUrl)
    if "" != publicUrlRefusal {
        return nil, exception.NewError(
            "the public url is refused",
            exceptioncontract.Context{"key": HttpPublicUrlKey, "value": publicUrl, "reason": publicUrlRefusal},
            nil,
        )
    }

    copiedStaticExcludedPaths := []string{}
    if nil != staticExcludedPaths {
        copiedStaticExcludedPaths = append([]string{}, staticExcludedPaths...)
    }

    httpConfigurationInstance := &httpConfiguration{
        address:                   address,
        publicUrl:                 normalizedPublicUrl,
        defaultLocale:             defaultLocale,
        publicDirectory:           publicDirectory,
        staticIndexFile:           staticIndexFile,
        maxRequestBodyBytes:       maxRequestBodyBytes,
        staticCacheDisabled:       staticCacheDisabled,
        staticCacheMaxAge:         staticCacheMaxAge,
        staticExcludedPaths:       copiedStaticExcludedPaths,
        sessionTtl:                sessionTtl,
        sessionTombstoneRetention: sessionTombstoneRetention,
        sessionStorageTimeout:     sessionStorageTimeout,
        sessionMaxSessions:        sessionMaxSessions,
        shutdownTimeout:           shutdownTimeout,
        serverLimits:              serverLimits,
    }

    validateErr := httpConfigurationInstance.validate()
    if nil != validateErr {
        return nil, validateErr
    }

    return httpConfigurationInstance, nil
}

type httpConfiguration struct {
    address                   string
    publicUrl                 string
    defaultLocale             string
    publicDirectory           string
    staticIndexFile           string
    maxRequestBodyBytes       bound.Resolved[int]
    staticCacheDisabled       bool
    staticCacheMaxAge         bound.Resolved[int]
    staticExcludedPaths       []string
    sessionTtl                bound.Resolved[time.Duration]
    sessionTombstoneRetention bound.Resolved[time.Duration]
    sessionStorageTimeout     bound.Resolved[time.Duration]
    sessionMaxSessions        bound.Resolved[int]
    shutdownTimeout           bound.Duration
    serverLimits              httpServerLimits
}

type httpServerLimits struct {
    readTimeout       bound.Duration
    readHeaderTimeout bound.Duration
    writeTimeout      bound.Duration
    idleTimeout       bound.Duration
    maxHeaderBytes    bound.Count

    unavailableRetryAfter bound.Duration
    sessionEmptyOnOutage  configcontract.Switch
}

func (instance *httpConfiguration) Address() string {
    return instance.address
}

/* PublicUrl is MELODY_HTTP_PUBLIC_URL as the deployment set it, its trailing slash trimmed, empty when unset. */
func (instance *httpConfiguration) PublicUrl() string {
    return instance.publicUrl
}

func (instance *httpConfiguration) DefaultLocale() string {
    return instance.defaultLocale
}

func (instance *httpConfiguration) PublicDirectory() string {
    return instance.publicDirectory
}

func (instance *httpConfiguration) StaticIndexFile() string {
    return instance.staticIndexFile
}

/* MaxRequestBodyBytes caps a request body, DefaultHttpMaxRequestBodyBytes when MELODY_HTTP_MAX_REQUEST_BODY_BYTES says nothing, at least one byte. */
func (instance *httpConfiguration) MaxRequestBodyBytes() bound.Resolved[int] {
    return instance.maxRequestBodyBytes
}

/* StaticCacheDisabled is MELODY_STATIC_CACHE_DISABLED: true sends no cache header and no validator, false, the default, the max age and the validators. */
func (instance *httpConfiguration) StaticCacheDisabled() bool {
    return instance.staticCacheDisabled
}

/* StaticCacheMaxAge is the max-age in seconds the built-in file server sends, DefaultStaticCacheMaxAge when MELODY_STATIC_CACHE_MAX_AGE says nothing, zero admitted. */
func (instance *httpConfiguration) StaticCacheMaxAge() bound.Resolved[int] {
    return instance.staticCacheMaxAge
}

/* StaticExcludedPaths names the request-path prefixes the built-in file server declines before it looks at the disk, so they reach the middleware registered with Use; the list is a copy. */
func (instance *httpConfiguration) StaticExcludedPaths() []string {
    return append([]string{}, instance.staticExcludedPaths...)
}

/* SessionTtl is how long a stored session stays valid since its last write, DefaultSessionTtl when MELODY_HTTP_SESSION_TTL says nothing. */
func (instance *httpConfiguration) SessionTtl() bound.Resolved[time.Duration] {
    return instance.sessionTtl
}

/* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back, at least 1s and never unlimited, DefaultSessionTombstoneRetention when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. */
func (instance *httpConfiguration) SessionTombstoneRetention() bound.Resolved[time.Duration] {
    return instance.sessionTombstoneRetention
}

/* SessionStorageTimeout bounds every session storage call, the load, the mint, the rotation, the save and the delete, at least 1ms or unlimited, DefaultSessionStorageTimeout when MELODY_HTTP_SESSION_STORAGE_TIMEOUT says nothing. */
func (instance *httpConfiguration) SessionStorageTimeout() bound.Resolved[time.Duration] {
    return instance.sessionStorageTimeout
}

/* SessionMaxSessions bounds the framework-wired in-memory storage, at least one or unlimited, DefaultSessionMaxSessions when MELODY_HTTP_SESSION_MAX_SESSIONS says nothing. */
func (instance *httpConfiguration) SessionMaxSessions() bound.Resolved[int] {
    return instance.sessionMaxSessions
}

/* ShutdownTimeout is MELODY_HTTP_SHUTDOWN_TIMEOUT as the deployment set it, absent when unset: at least 1ms and never unlimited. */
func (instance *httpConfiguration) ShutdownTimeout() bound.Duration {
    return instance.shutdownTimeout
}

/* ReadTimeout is MELODY_HTTP_READ_TIMEOUT as the deployment set it, absent when unset. */
func (instance *httpConfiguration) ReadTimeout() bound.Duration {
    return instance.serverLimits.readTimeout
}

func (instance *httpConfiguration) ReadHeaderTimeout() bound.Duration {
    return instance.serverLimits.readHeaderTimeout
}

func (instance *httpConfiguration) WriteTimeout() bound.Duration {
    return instance.serverLimits.writeTimeout
}

func (instance *httpConfiguration) IdleTimeout() bound.Duration {
    return instance.serverLimits.idleTimeout
}

func (instance *httpConfiguration) MaxHeaderBytes() bound.Count {
    return instance.serverLimits.maxHeaderBytes
}

/* UnavailableRetryAfter is MELODY_HTTP_UNAVAILABLE_RETRY_AFTER as the deployment set it, absent when unset. */
func (instance *httpConfiguration) UnavailableRetryAfter() bound.Duration {
    return instance.serverLimits.unavailableRetryAfter
}

/* SessionEmptyOnOutage is MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE as the deployment set it, unset when the key says nothing. */
func (instance *httpConfiguration) SessionEmptyOnOutage() configcontract.Switch {
    return instance.serverLimits.sessionEmptyOnOutage
}

func (instance *httpConfiguration) validate() error {
    validateAddressErr := instance.validateAddress()
    if nil != validateAddressErr {
        return validateAddressErr
    }

    validateDefaultLocaleErr := instance.validateDefaultLocale()
    if nil != validateDefaultLocaleErr {
        return validateDefaultLocaleErr
    }

    validatePublicDirectoryErr := instance.validatePublicDirectory()
    if nil != validatePublicDirectoryErr {
        return validatePublicDirectoryErr
    }

    validateStaticIndexFileErr := instance.validateStaticIndexFile()
    if nil != validateStaticIndexFileErr {
        return validateStaticIndexFileErr
    }

    validateStaticExcludedPathsErr := instance.validateStaticExcludedPaths()
    if nil != validateStaticExcludedPathsErr {
        return validateStaticExcludedPathsErr
    }

    return nil
}

func (instance *httpConfiguration) validateAddress() error {
    address := instance.address
    if "" == address {
        return exception.NewError("http address may not be empty", nil, nil)
    }

    _, portString, splitHostPortErr := net.SplitHostPort(address)
    if nil != splitHostPortErr {
        return exception.NewError(
            "http address is invalid",
            exceptioncontract.Context{
                "address": address,
            },
            splitHostPortErr,
        )
    }

    port, atoiErr := strconv.Atoi(portString)
    if nil != atoiErr {
        return exception.NewError(
            "http port is invalid",
            exceptioncontract.Context{
                "address": address,
                "port":    portString,
            },
            atoiErr,
        )
    }

    if 1 > port || 65535 < port {
        return exception.NewError(
            "http port is out of range",
            exceptioncontract.Context{
                "address": address,
                "port":    port,
            },
            nil,
        )
    }

    return nil
}

func (instance *httpConfiguration) validateDefaultLocale() error {
    defaultLocale := instance.defaultLocale
    if "" == defaultLocale {
        return exception.NewError("default locale may not be empty", nil, nil)
    }

    if false == defaultLocalePattern.MatchString(defaultLocale) {
        return exception.NewError(
            "default locale is invalid",
            exceptioncontract.Context{
                "defaultLocale": defaultLocale,
            },
            nil,
        )
    }

    return nil
}

func (instance *httpConfiguration) validatePublicDirectory() error {
    publicDirectory := instance.publicDirectory
    if "" == publicDirectory {
        return exception.NewError("public directory may not be empty", nil, nil)
    }

    if true == strings.Contains(publicDirectory, "..") {
        return exception.NewError(
            "public directory is invalid",
            exceptioncontract.Context{
                "publicDirectory": publicDirectory,
            },
            nil,
        )
    }

    return nil
}

func (instance *httpConfiguration) validateStaticIndexFile() error {
    staticIndexFile := instance.staticIndexFile
    if "" == staticIndexFile {
        return exception.NewError("static index file may not be empty", nil, nil)
    }

    if true == strings.Contains(staticIndexFile, "/") || true == strings.Contains(staticIndexFile, `\`) {
        return exception.NewError(
            "static index file is invalid",
            exceptioncontract.Context{
                "staticIndexFile": staticIndexFile,
            },
            nil,
        )
    }

    return nil
}

/* %2F is the one escape admitted: the decoded comparison never matches any other */
func (instance *httpConfiguration) validateStaticExcludedPaths() error {
    for index, excludedPath := range instance.staticExcludedPaths {
        if "" == excludedPath {
            return exception.NewError(
                "static excluded path may not be empty",
                exceptioncontract.Context{
                    "staticExcludedPaths": instance.staticExcludedPaths,
                },
                nil,
            )
        }

        if false == strings.HasPrefix(excludedPath, "/") {
            return exception.NewError(
                "static excluded path must begin with a slash",
                exceptioncontract.Context{
                    "excludedPath": excludedPath,
                },
                nil,
            )
        }

        if true == internal.CarriesPercentEscapeOtherThanSeparator(excludedPath) {
            return exception.NewError(
                "static excluded path carries a percent escape: request paths are matched decoded, with %2F for a separator inside a segment, so the entry excludes only a path whose name holds the percent sign literally; write it decoded",
                exceptioncontract.Context{
                    "environmentKey": StaticExcludedPathsKey,
                    "index":          index,
                    "excludedPath":   excludedPath,
                },
                nil,
            )
        }
    }

    return nil
}

/* empty once trimmed is no list, not one empty entry naming everything */
func splitConfigurationList(value string) []string {
    trimmedValue := strings.TrimSpace(value)
    if "" == trimmedValue {
        return []string{}
    }

    entries := strings.Split(trimmedValue, ",")

    list := make([]string, 0, len(entries))
    for _, entry := range entries {
        list = append(list, strings.TrimSpace(entry))
    }

    return list
}

var _ configcontract.HttpConfiguration = (*httpConfiguration)(nil)
