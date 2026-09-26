package config

import (
    "net"
    "regexp"
    "strconv"
    "strings"
    "time"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

var (
    defaultLocalePattern = regexp.MustCompile(`^[a-z]{2}(-[A-Za-z]{2})?$`)
)

/* MinimumSessionTtl is the shortest session lifetime that can still describe a session: the storage purges every lapsed entry on the write that stores the new one, so a smaller ttl makes SaveSession report success and persist nothing. Zero keeps its meaning of "no expiry". */
const MinimumSessionTtl = time.Second

/* DefaultSessionTtl is the lifetime a stored session gets when MELODY_HTTP_SESSION_TTL says nothing: zero, no expiry. Melody mints a session for every request without a session cookie, so once an application writes a session on a public path an unbounded lifetime makes every cookie-less request a permanent entry, which the default in-memory storage cannot expire; the application warns at boot on that combination. Set this to what the deployment wants. */
const DefaultSessionTtl = 0 * time.Second

/* DefaultSessionTombstoneRetention is how long a deleted session id keeps refusing a write-back when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. The window has to cover the longest a request can hold a session snapshot loaded before the delete, since nothing bounds a handler's lifetime; a deployment with slower requests raises it, at one remembered entry per deletion. The record lives in the manager, per process. */
const DefaultSessionTombstoneRetention = 5 * time.Minute

/* DefaultHttpShutdownTimeout is how long a stopping http server waits for the requests already admitted when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. It is far below the write timeout; a deployment whose supervisor grants a longer grace raises it to match. */
const DefaultHttpShutdownTimeout = 5 * time.Second

func newHttpConfiguration(
    address string,
    defaultLocale string,
    publicDir string,
    staticIndexFile string,
    maxRequestBodyBytes int,
    staticEnableCache bool,
    staticCacheMaxAge int,
    staticExcludedPaths []string,
    sessionTtl time.Duration,
    sessionTombstoneRetention time.Duration,
    shutdownTimeout time.Duration,
) (*httpConfiguration, error) {
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
        shutdownTimeout:           shutdownTimeout,
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
    maxRequestBodyBytes       int
    staticEnableCache         bool
    staticCacheMaxAge         int
    staticExcludedPaths       []string
    sessionTtl                time.Duration
    sessionTombstoneRetention time.Duration
    shutdownTimeout           time.Duration
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

func (instance *httpConfiguration) MaxRequestBodyBytes() int {
    return instance.maxRequestBodyBytes
}

func (instance *httpConfiguration) StaticEnableCache() bool {
    return instance.staticEnableCache
}

func (instance *httpConfiguration) StaticCacheMaxAge() int {
    return instance.staticCacheMaxAge
}

/* StaticExcludedPaths names the path prefixes the built-in file server declines before it looks at the disk. The file server sits outermost, so a declined prefix reaches the middleware registered with Use, which is how an application takes part of the url back. An entry is a request-path prefix as security.NewPathPrefixMatcher reads one. The list is returned as a copy. */
func (instance *httpConfiguration) StaticExcludedPaths() []string {
    return append([]string{}, instance.staticExcludedPaths...)
}

/* SessionTtl is how long a stored session stays valid, DefaultSessionTtl when MELODY_HTTP_SESSION_TTL says nothing. The clock runs from the last write, and reading a session does not refresh it. Zero stores the session without any expiry. */
func (instance *httpConfiguration) SessionTtl() time.Duration {
    return instance.sessionTtl
}

/* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back, DefaultSessionTombstoneRetention when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing: a request that outlives it can save the deleted session back with the pre-logout identity. Zero and negative fail the boot, since they would disarm the logout defence. */
func (instance *httpConfiguration) SessionTombstoneRetention() time.Duration {
    return instance.sessionTombstoneRetention
}

/* ShutdownTimeout is how long a stopping http server waits for the requests it has admitted before cutting them, DefaultHttpShutdownTimeout when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. Exceeding it is a shutdown failure and a non-zero exit; zero and negative fail the boot. */
func (instance *httpConfiguration) ShutdownTimeout() time.Duration {
    return instance.shutdownTimeout
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

    validateMaxRequestBodyBytesErr := instance.validateMaxRequestBodyBytes()
    if nil != validateMaxRequestBodyBytesErr {
        return validateMaxRequestBodyBytesErr
    }

    validateStaticCacheMaxAgeErr := instance.validateStaticCacheMaxAge()
    if nil != validateStaticCacheMaxAgeErr {
        return validateStaticCacheMaxAgeErr
    }

    validateStaticExcludedPathsErr := instance.validateStaticExcludedPaths()
    if nil != validateStaticExcludedPathsErr {
        return validateStaticExcludedPathsErr
    }

    validateSessionTtlErr := instance.validateSessionTtl()
    if nil != validateSessionTtlErr {
        return validateSessionTtlErr
    }

    validateSessionTombstoneRetentionErr := instance.validateSessionTombstoneRetention()
    if nil != validateSessionTombstoneRetentionErr {
        return validateSessionTombstoneRetentionErr
    }

    validateShutdownTimeoutErr := instance.validateShutdownTimeout()
    if nil != validateShutdownTimeoutErr {
        return validateShutdownTimeoutErr
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

func (instance *httpConfiguration) validateMaxRequestBodyBytes() error {
    if 0 >= instance.maxRequestBodyBytes {
        return exception.NewError(
            "invalid http max request body bytes",
            exceptioncontract.Context{
                "value": instance.maxRequestBodyBytes,
            },
            nil,
        )
    }

    return nil
}

func (instance *httpConfiguration) validateStaticCacheMaxAge() error {
    if 0 > instance.staticCacheMaxAge {
        return exception.NewError(
            "static cache max age must be zero or positive",
            exceptioncontract.Context{
                "staticCacheMaxAge": instance.staticCacheMaxAge,
            },
            nil,
        )
    }

    return nil
}

/* an excluded path must be shaped like the beginning of a request path: an entry without a leading slash can never match, and an empty entry matches every path, taking the whole file server out of service */
func (instance *httpConfiguration) validateStaticExcludedPaths() error {
    for _, excludedPath := range instance.staticExcludedPaths {
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
    }

    return nil
}

func (instance *httpConfiguration) validateSessionTtl() error {
    if 0 > instance.sessionTtl {
        return exception.NewError(
            "session ttl must be zero or positive",
            exceptioncontract.Context{
                "sessionTtl": instance.sessionTtl.String(),
            },
            nil,
        )
    }

    if 0 < instance.sessionTtl && MinimumSessionTtl > instance.sessionTtl {
        return exception.NewError(
            "session ttl is positive but shorter than one second, which stores no usable session; use zero for no expiry",
            exceptioncontract.Context{
                "sessionTtl": instance.sessionTtl.String(),
                "minimum":    MinimumSessionTtl.String(),
            },
            nil,
        )
    }

    return nil
}

/* only a positive window can refuse anything; zero or negative would disarm the write-back defence, so it is refused rather than normalized */
func (instance *httpConfiguration) validateSessionTombstoneRetention() error {
    if 0 >= instance.sessionTombstoneRetention {
        return exception.NewError(
            "http session tombstone retention must be positive",
            exceptioncontract.Context{
                "sessionTombstoneRetention": instance.sessionTombstoneRetention.String(),
                "default":                   DefaultSessionTombstoneRetention.String(),
            },
            nil,
        )
    }

    return nil
}

/* only a positive duration can describe a wait, and zero has no other meaning here, so zero and negative are refused rather than normalized */
func (instance *httpConfiguration) validateShutdownTimeout() error {
    if 0 >= instance.shutdownTimeout {
        return exception.NewError(
            "http shutdown timeout must be positive",
            exceptioncontract.Context{
                "shutdownTimeout": instance.shutdownTimeout.String(),
                "default":         DefaultHttpShutdownTimeout.String(),
            },
            nil,
        )
    }

    return nil
}

/* a list is one environment value separated by commas, each entry trimmed, since a request path carries no surrounding spaces; a value empty once trimmed is no list at all, not a list of one empty entry that would name everything */
func splitHttpConfigurationList(value string) []string {
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
