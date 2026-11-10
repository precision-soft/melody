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

/* DefaultSessionTtl is the lifetime a stored session gets when MELODY_HTTP_SESSION_TTL says nothing. Melody mints a session for every request without a session cookie, so the default is bounded: an unbounded lifetime makes every cookie-less request on a path that writes a session a permanent entry, which the default in-memory storage cannot expire. A deployment that wants no expiry writes unlimited, and the application warns at boot on that combination. The ttl is at least one second: the storages purge a lapsed entry on the next write or load, so a shorter one stores no usable session. */
const DefaultSessionTtl = 24 * time.Hour

/* DefaultHttpMaxRequestBodyBytes is the request body cap when MELODY_HTTP_MAX_REQUEST_BODY_BYTES says nothing, 1 MiB. */
const DefaultHttpMaxRequestBodyBytes = 1 << 20

/* DefaultStaticCacheMaxAge is the max-age in seconds the built-in file server sends when MELODY_STATIC_CACHE_MAX_AGE says nothing; zero is a value, a response marked fresh for no time. */
const DefaultStaticCacheMaxAge = 3600

/* the five per-request server limits an http application runs with when neither MELODY_HTTP_* nor the application's ApplicationConfig names one; a slow client is cut instead of holding a connection open. net/http arms the write timeout once, from the request line, so a streaming handler re-arms it per frame through the event-stream writer's write budget. */
const (
    DefaultHttpReadTimeout       = 15 * time.Second
    DefaultHttpReadHeaderTimeout = 5 * time.Second
    DefaultHttpWriteTimeout      = 30 * time.Second
    DefaultHttpIdleTimeout       = 60 * time.Second
    DefaultHttpMaxHeaderBytes    = 1 << 20
)

/* DefaultSessionTombstoneRetention is how long a deleted session id keeps refusing a write-back when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. The window has to cover the longest a request can hold a session snapshot loaded before the delete, since nothing bounds a handler's lifetime; a deployment with slower requests raises it, at one remembered entry per deletion. The record lives in the manager, per process. */
const DefaultSessionTombstoneRetention = 5 * time.Minute

/* DefaultSessionMaxSessions is how many sessions the framework-wired in-memory storage holds at once when MELODY_HTTP_SESSION_MAX_SESSIONS says nothing; at the ceiling a new session id is refused. */
const DefaultSessionMaxSessions = 100_000

/* DefaultUnavailableRetryAfter is the Retry-After of a 503 answering an infrastructure failure outside a firewall when neither MELODY_HTTP_UNAVAILABLE_RETRY_AFTER nor ApplicationConfig names one; a firewall's own delay has the same default. */
const DefaultUnavailableRetryAfter = 5 * time.Second

/* DefaultHttpShutdownTimeout is how long a stopping http server waits for the requests already admitted when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. It is far below the write timeout; a deployment whose supervisor grants a longer grace raises it to match. */
const DefaultHttpShutdownTimeout = 5 * time.Second

func newHttpConfiguration(
    address string,
    defaultLocale string,
    publicDir string,
    staticIndexFile string,
    maxRequestBodyBytes bound.Resolved[int],
    staticEnableCache bool,
    staticCacheMaxAge bound.Resolved[int],
    staticExcludedPaths []string,
    sessionTtl bound.Resolved[time.Duration],
    sessionTombstoneRetention bound.Resolved[time.Duration],
    sessionMaxSessions bound.Resolved[int],
    shutdownTimeout bound.Resolved[time.Duration],
    serverLimits httpServerLimits,
) (*httpConfiguration, error) {
    /* an empty address is refused by name here, before the ":" prefix below would turn it into a port refusal over an address the caller never wrote */
    if "" == strings.TrimSpace(address) {
        return nil, exception.NewError("http address may not be empty", nil, nil)
    }

    if false == strings.Contains(address, ":") {
        address = ":" + address
    }

    copiedStaticExcludedPaths := []string{}
    if nil != staticExcludedPaths {
        copiedStaticExcludedPaths = append([]string{}, staticExcludedPaths...)
    }

    httpConfigurationInstance := &httpConfiguration{
        address:                   address,
        defaultLocale:             defaultLocale,
        publicDir:                 publicDir,
        staticIndexFile:           staticIndexFile,
        maxRequestBodyBytes:       maxRequestBodyBytes,
        staticEnableCache:         staticEnableCache,
        staticCacheMaxAge:         staticCacheMaxAge,
        staticExcludedPaths:       copiedStaticExcludedPaths,
        sessionTtl:                sessionTtl,
        sessionTombstoneRetention: sessionTombstoneRetention,
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
    defaultLocale             string
    publicDir                 string
    staticIndexFile           string
    maxRequestBodyBytes       bound.Resolved[int]
    staticEnableCache         bool
    staticCacheMaxAge         bound.Resolved[int]
    staticExcludedPaths       []string
    sessionTtl                bound.Resolved[time.Duration]
    sessionTombstoneRetention bound.Resolved[time.Duration]
    sessionMaxSessions        bound.Resolved[int]
    shutdownTimeout           bound.Resolved[time.Duration]
    serverLimits              httpServerLimits
}

type httpServerLimits struct {
    readTimeout       bound.Duration
    readHeaderTimeout bound.Duration
    writeTimeout      bound.Duration
    idleTimeout       bound.Duration
    maxHeaderBytes    bound.Count

    unavailableRetryAfter bound.Duration
}

func (instance *httpConfiguration) Address() string {
    return instance.address
}

func (instance *httpConfiguration) DefaultLocale() string {
    return instance.defaultLocale
}

func (instance *httpConfiguration) PublicDir() string {
    return instance.publicDir
}

func (instance *httpConfiguration) StaticIndexFile() string {
    return instance.staticIndexFile
}

/* MaxRequestBodyBytes caps a request body, DefaultHttpMaxRequestBodyBytes when MELODY_HTTP_MAX_REQUEST_BODY_BYTES says nothing, at least one byte. */
func (instance *httpConfiguration) MaxRequestBodyBytes() bound.Resolved[int] {
    return instance.maxRequestBodyBytes
}

func (instance *httpConfiguration) StaticEnableCache() bool {
    return instance.staticEnableCache
}

/* StaticCacheMaxAge is the max-age in seconds the built-in file server sends, DefaultStaticCacheMaxAge when MELODY_STATIC_CACHE_MAX_AGE says nothing, zero admitted. */
func (instance *httpConfiguration) StaticCacheMaxAge() bound.Resolved[int] {
    return instance.staticCacheMaxAge
}

/* StaticExcludedPaths names the path prefixes the built-in file server declines before it looks at the disk. The file server sits outermost, so a declined prefix reaches the middleware registered with Use, which is how an application takes part of the url back. An entry is a request-path prefix as security.NewPathPrefixMatcher reads one. The list is returned as a copy. */
func (instance *httpConfiguration) StaticExcludedPaths() []string {
    return append([]string{}, instance.staticExcludedPaths...)
}

/* SessionTtl is how long a stored session stays valid, DefaultSessionTtl when MELODY_HTTP_SESSION_TTL says nothing, and unlimited when it says unlimited. The clock runs from the last write, and reading a session does not refresh it. */
func (instance *httpConfiguration) SessionTtl() bound.Resolved[time.Duration] {
    return instance.sessionTtl
}

/* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back, DefaultSessionTombstoneRetention when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing: a request that outlives it can save the deleted session back with the pre-logout identity. It is at least one second and never unlimited, since a shorter window would disarm the logout defence. */
func (instance *httpConfiguration) SessionTombstoneRetention() bound.Resolved[time.Duration] {
    return instance.sessionTombstoneRetention
}

/* SessionMaxSessions is how many sessions the framework-wired in-memory storage holds at once, DefaultSessionMaxSessions when MELODY_HTTP_SESSION_MAX_SESSIONS says nothing, at least one, unlimited admitted; a storage the application registers declares its own bound. */
func (instance *httpConfiguration) SessionMaxSessions() bound.Resolved[int] {
    return instance.sessionMaxSessions
}

/* ShutdownTimeout is how long a stopping http server waits for the requests it has admitted before cutting them, DefaultHttpShutdownTimeout when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. Exceeding it is a shutdown failure and a non-zero exit. It is at least 1ms and never unlimited. */
func (instance *httpConfiguration) ShutdownTimeout() bound.Resolved[time.Duration] {
    return instance.shutdownTimeout
}

/* ReadTimeout is MELODY_HTTP_READ_TIMEOUT as the deployment set it, absent when the key says nothing; the application decides it against its own config and HttpReadTimeoutRule. */
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

/* UnavailableRetryAfter is MELODY_HTTP_UNAVAILABLE_RETRY_AFTER as the deployment set it, absent when the key says nothing; the application decides it against its own config and UnavailableRetryAfterRule. */
func (instance *httpConfiguration) UnavailableRetryAfter() bound.Duration {
    return instance.serverLimits.unavailableRetryAfter
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

    validatePublicDirErr := instance.validatePublicDir()
    if nil != validatePublicDirErr {
        return validatePublicDirErr
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

func (instance *httpConfiguration) validatePublicDir() error {
    publicDir := instance.publicDir
    if "" == publicDir {
        return exception.NewError("public directory may not be empty", nil, nil)
    }

    if true == strings.Contains(publicDir, "..") {
        return exception.NewError(
            "public directory is invalid",
            exceptioncontract.Context{
                "publicDir": publicDir,
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

/* refused: an entry that cannot begin a request path, an empty one that excludes everything, and a percent escape other than %2F, which the decoded comparison never matches */
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

/* a value empty once trimmed is no list, not one empty entry that would name everything */
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
