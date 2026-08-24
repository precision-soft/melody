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

/* MinimumSessionTtl is the shortest session lifetime that can still describe a session. Below it the value is not a short session, it is a broken one: the storage purges every lapsed entry on the write that stores the new one, so a ttl smaller than the time that write takes makes SaveSession report success and persist nothing — a login that answers "welcome" and leaves the user logged out. A session also has to survive the response reaching the client and the client coming back, which no sub-second lifetime does, and a second is the finest unit http itself dates anything in. Zero keeps its own meaning of "no expiry" and is not affected. */
const MinimumSessionTtl = time.Second

/* DefaultSessionTtl is the lifetime a stored session gets when MELODY_HTTP_SESSION_TTL says nothing. It is zero — no expiry — which is what every deployment that predates the setting already had, so upgrading does not start logging users out at a lifetime nobody chose.

Zero is not free of hazard, and the hazard is worth naming here rather than discovering in a memory graph: melody mints a session for every request that arrives without a session cookie, so once an application writes to a session on a public path — a csrf token, a flash message, a locale — an unbounded lifetime turns every cookie-less request into a permanent entry. That is survivable in a shared store an operator can expire, and it is not in the default in-memory one, which is why the application warns at boot when it finds both together rather than quietly picking a lifetime on the deployment's behalf. Set this to what the deployment actually wants. */
const DefaultSessionTtl = 0 * time.Second

/* DefaultSessionTombstoneRetention is how long a deleted session id keeps refusing a write-back when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. The window has to cover the longest a request of the deployment can still be holding a session snapshot loaded before the delete — nothing in the chain bounds a handler's lifetime, the server's socket timeouts cut the connection but not the goroutine — so a deployment whose slowest legitimate request outlives five minutes raises this to match, at the cost of one remembered entry per deletion within the window. The record lives in the manager, per process. */
const DefaultSessionTombstoneRetention = 5 * time.Minute

/* DefaultHttpShutdownTimeout is how long a stopping http server waits for the requests already admitted when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. Five seconds is deliberately far below the thirty the write timeout promises each request: a deployment whose supervisor grants a longer termination grace raises this to match, and one that leaves both at their defaults trades the tail of the slowest requests for a process that is gone before the supervisor escalates. */
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

/* StaticExcludedPaths names the path prefixes the built-in file server declines before it looks at the disk. The built-in server sits outermost in the pipeline, so whatever it declines is what reaches the middleware an application registers with Use: excluding a prefix is how an application takes a part of the url back — to put authentication in front of a directory, to apply a narrower dot-prefix policy, or to serve it from a root of its own. An entry is a prefix of the request path exactly as security.NewPathPrefixMatcher reads one, so the same spelling selects the same requests here and in a firewall rule. The list is returned as a copy because the configuration is read by every request while the caller is free to keep the slice. */
func (instance *httpConfiguration) StaticExcludedPaths() []string {
    return append([]string{}, instance.staticExcludedPaths...)
}

/* SessionTtl is how long a stored session stays valid, DefaultSessionTtl when MELODY_HTTP_SESSION_TTL says nothing. The clock runs from the last write, not from the last request, and reading a session does not refresh it: a session written on every request renews itself, while one written once at login lapses this long after that write however active the visitor was. Zero stores the session without any expiry and is available as an explicit choice. */
func (instance *httpConfiguration) SessionTtl() time.Duration {
    return instance.sessionTtl
}

/* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back, DefaultSessionTombstoneRetention when MELODY_HTTP_SESSION_TOMBSTONE_RETENTION says nothing. It is sized to the longest a request of this deployment can still be holding a session snapshot loaded before the delete: a request that outlives it can save the deleted session back with the pre-logout identity intact. Only a positive value can describe the window, so zero and negative fail the boot instead of silently disarming the logout defence. */
func (instance *httpConfiguration) SessionTombstoneRetention() time.Duration {
    return instance.sessionTombstoneRetention
}

/* ShutdownTimeout is how long a stopping http server waits for the requests it has already admitted before cutting them, DefaultHttpShutdownTimeout when MELODY_HTTP_SHUTDOWN_TIMEOUT says nothing. Exceeding it is reported as a shutdown failure and the process exits non-zero, because requests were lost; only a positive value can describe a wait, so zero and negative fail the boot instead of silently becoming the default. */
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

/* an excluded path is compared against the request path the way security.NewPathPrefixMatcher compares one, so it has to be shaped like the beginning of a path. A request path always starts with a slash, so an entry that does not can never match, and the application that wrote it would go on believing a directory is hers while the file server keeps answering for it. An empty entry is refused for the opposite reason: the prefix comparison matches every path against it, so one stray comma would silently take the whole file server out of service. */
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

/* only a positive window can refuse anything: zero or negative would disarm the write-back defence entirely, which is not a shorter window but a different and dangerous meaning, so it is refused rather than silently normalized to a default the operator did not choose. */
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

/* only a positive duration can describe a wait, and unlike the session ttl there is no meaning left over for zero: a deployment that wants no graceful window says so with a value as small as it likes, while zero and negative are refused rather than silently normalized to a default the operator did not choose. */
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

/* a list arrives as one environment value, and the comma is the separator melody already reads lists with — an accept header, an entity tag list, the redis address list — and the one an .env line carries without quoting. Each entry is trimmed because a list written to stay readable carries spaces the request path never has, so an untrimmed entry would silently match nothing. A value that is empty once trimmed is no list at all rather than a list of one empty entry, which is the difference between naming nothing and naming everything. Nothing here interprets the entry, so a pattern language added later reads through the same key and the same separator. */
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
