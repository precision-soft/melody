package config

import (
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func newConfigurationFromValues(values map[string]string) (*Configuration, error) {
    environment, err := NewEnvironment(&testEnvironmentSource{values: values})
    if nil != err {
        return nil, err
    }

    return NewConfiguration(environment, "/tmp/melody")
}

func mustConfigurationFromValues(t *testing.T, values map[string]string) *Configuration {
    t.Helper()

    configuration, err := newConfigurationFromValues(values)
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    return configuration
}

func TestConfigurationHttpLimitsTakeTheirDefaultsWhenLeftOut(t *testing.T) {
    httpConfiguration := mustConfigurationFromValues(t, map[string]string{}).Http()

    if (bound.Resolved[int]{Value: 1 << 20}) != httpConfiguration.MaxRequestBodyBytes() {
        t.Fatalf("expected a 1 MiB body cap, got %+v", httpConfiguration.MaxRequestBodyBytes())
    }
    if (bound.Resolved[int]{Value: 3600}) != httpConfiguration.StaticCacheMaxAge() {
        t.Fatalf("expected a 3600s cache max age, got %+v", httpConfiguration.StaticCacheMaxAge())
    }
    if (bound.Resolved[time.Duration]{Value: 24 * time.Hour}) != httpConfiguration.SessionTtl() {
        t.Fatalf("expected a 24h session ttl, got %+v", httpConfiguration.SessionTtl())
    }
    if (bound.Resolved[time.Duration]{Value: 5 * time.Minute}) != httpConfiguration.SessionTombstoneRetention() {
        t.Fatalf("expected a 5m tombstone retention, got %+v", httpConfiguration.SessionTombstoneRetention())
    }
    if (bound.Resolved[time.Duration]{Value: 5 * time.Second}) != httpConfiguration.ShutdownTimeout() {
        t.Fatalf("expected a 5s shutdown timeout, got %+v", httpConfiguration.ShutdownTimeout())
    }
}

func TestConfigurationHttpLimitsAreReadFromTheEnvironment(t *testing.T) {
    httpConfiguration := mustConfigurationFromValues(t, map[string]string{
        HttpMaxRequestBodyBytesKey:       "2048",
        StaticCacheMaxAgeKey:             "0",
        HttpSessionTtlKey:                "30m",
        HttpSessionTombstoneRetentionKey: "1s",
        HttpShutdownTimeoutKey:           "45s",
    }).Http()

    if (bound.Resolved[int]{Value: 2048}) != httpConfiguration.MaxRequestBodyBytes() {
        t.Fatalf("unexpected body cap %+v", httpConfiguration.MaxRequestBodyBytes())
    }
    if (bound.Resolved[int]{Value: 0}) != httpConfiguration.StaticCacheMaxAge() {
        t.Fatalf("expected a zero cache max age kept as a value, got %+v", httpConfiguration.StaticCacheMaxAge())
    }
    if (bound.Resolved[time.Duration]{Value: 30 * time.Minute}) != httpConfiguration.SessionTtl() {
        t.Fatalf("unexpected session ttl %+v", httpConfiguration.SessionTtl())
    }
    if (bound.Resolved[time.Duration]{Value: time.Second}) != httpConfiguration.SessionTombstoneRetention() {
        t.Fatalf("unexpected tombstone retention %+v", httpConfiguration.SessionTombstoneRetention())
    }
    if (bound.Resolved[time.Duration]{Value: 45 * time.Second}) != httpConfiguration.ShutdownTimeout() {
        t.Fatalf("unexpected shutdown timeout %+v", httpConfiguration.ShutdownTimeout())
    }
}

func TestConfigurationHttpSessionTtlReadsUnlimitedAsNoExpiry(t *testing.T) {
    sessionTtl := mustConfigurationFromValues(t, map[string]string{HttpSessionTtlKey: "unlimited"}).Http().SessionTtl()
    if false == sessionTtl.Unlimited {
        t.Fatalf("expected unlimited, got %+v", sessionTtl)
    }
}

func TestConfigurationHttpLimitsRefuseWhatTheirRuleDoesNotAdmit(t *testing.T) {
    cases := []struct {
        key   string
        value string
        field string
    }{
        {HttpMaxRequestBodyBytesKey, "0", "HttpConfiguration.MaxRequestBodyBytes"},
        {HttpMaxRequestBodyBytesKey, "unlimited", "HttpConfiguration.MaxRequestBodyBytes"},
        {StaticCacheMaxAgeKey, "-1", "HttpConfiguration.StaticCacheMaxAge"},
        {StaticCacheMaxAgeKey, "unlimited", "HttpConfiguration.StaticCacheMaxAge"},
        {HttpSessionTtlKey, "0", "HttpConfiguration.SessionTtl"},
        {HttpSessionTtlKey, "-1m", "HttpConfiguration.SessionTtl"},
        {HttpSessionTtlKey, "999ms", "HttpConfiguration.SessionTtl"},
        {HttpSessionTombstoneRetentionKey, "999ms", "HttpConfiguration.SessionTombstoneRetention"},
        {HttpSessionTombstoneRetentionKey, "unlimited", "HttpConfiguration.SessionTombstoneRetention"},
        {HttpShutdownTimeoutKey, "0", "HttpConfiguration.ShutdownTimeout"},
        {HttpShutdownTimeoutKey, "unlimited", "HttpConfiguration.ShutdownTimeout"},
        {HttpReadTimeoutKey, "0", "ApplicationConfig.ReadTimeout"},
        {HttpReadHeaderTimeoutKey, "unlimited", "ApplicationConfig.ReadHeaderTimeout"},
        {HttpWriteTimeoutKey, "-1s", "ApplicationConfig.WriteTimeout"},
        {HttpIdleTimeoutKey, "0s", "ApplicationConfig.IdleTimeout"},
        {HttpMaxHeaderBytesKey, "4095", "ApplicationConfig.MaxHeaderBytes"},
        {HttpReadTimeoutKey, "unlimited", "ApplicationConfig.ReadTimeout"},
        {HttpWriteTimeoutKey, "unlimited", "ApplicationConfig.WriteTimeout"},
        {HttpIdleTimeoutKey, "unlimited", "ApplicationConfig.IdleTimeout"},
        {HttpMaxHeaderBytesKey, "unlimited", "ApplicationConfig.MaxHeaderBytes"},
        {HttpReadHeaderTimeoutKey, "999us", "ApplicationConfig.ReadHeaderTimeout"},
        {TeardownTimeoutKey, "0", "ApplicationConfig.TeardownTimeout"},
    }

    for _, testCase := range cases {
        _, err := newConfigurationFromValues(map[string]string{testCase.key: testCase.value})
        assertLimitRefusal(t, err, testCase.key, testCase.field)
    }
}

func TestConfigurationHttpLimitsRefuseAnUnparsableValueNamingTheKey(t *testing.T) {
    for _, key := range []string{HttpShutdownTimeoutKey, HttpMaxRequestBodyBytesKey, HttpReadTimeoutKey, TeardownTimeoutKey} {
        _, err := newConfigurationFromValues(map[string]string{key: "soon"})
        assertLimitRefusal(t, err, key, "")
    }
}

func TestConfigurationApplicationLimitsAreAbsentUnlessSet(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    if false == configuration.Http().ReadTimeout().IsAbsent() || false == configuration.Http().MaxHeaderBytes().IsAbsent() || false == configuration.Kernel().TeardownTimeout().IsAbsent() {
        t.Fatalf("expected the application limits absent when no key sets them")
    }

    configuration = mustConfigurationFromValues(t, map[string]string{
        HttpReadTimeoutKey:       "20s",
        HttpReadHeaderTimeoutKey: "2s",
        HttpWriteTimeoutKey:      "40s",
        HttpIdleTimeoutKey:       "90s",
        HttpMaxHeaderBytesKey:    "4096",
        TeardownTimeoutKey:       "unlimited",
    })
    if bound.Of(20*time.Second) != configuration.Http().ReadTimeout() ||
        bound.Of(2*time.Second) != configuration.Http().ReadHeaderTimeout() ||
        bound.Of(40*time.Second) != configuration.Http().WriteTimeout() ||
        bound.Of(90*time.Second) != configuration.Http().IdleTimeout() ||
        bound.Of(4096) != configuration.Http().MaxHeaderBytes() ||
        false == configuration.Kernel().TeardownTimeout().IsUnlimited() {
        t.Fatalf("expected the application limits as set, got %v %v %v %v %v %v", configuration.Http().ReadTimeout(), configuration.Http().ReadHeaderTimeout(), configuration.Http().WriteTimeout(), configuration.Http().IdleTimeout(), configuration.Http().MaxHeaderBytes(), configuration.Kernel().TeardownTimeout())
    }

    if "3s" != mustConfigurationFromValues(t, map[string]string{HttpReadTimeoutKey: "3s"}).MustGet(KernelHttpReadTimeout).MustString() {
        t.Fatalf("expected the kernel alias to carry the read timeout")
    }
}

func assertLimitRefusal(t *testing.T, err error, environmentKey string, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("%s: expected a refusal", environmentKey)
    }

    foundKey, foundField := false, "" == field
    for current := err; nil != current; current = errors.Unwrap(current) {
        refusal, isRefusal := current.(*exception.Error)
        if false == isRefusal {
            continue
        }

        if environmentKey == refusal.Context()["environmentKey"] {
            foundKey = true
        }

        if "" != field && field == refusal.Context()["field"] {
            foundField = true
        }
    }

    if false == foundKey || false == foundField {
        t.Fatalf("%s: expected the refusal to name the key and the field %q, got %v", environmentKey, field, err)
    }
}

func TestConfigurationHttpStaticExcludedPathsDefaultsToEmpty(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    excludedPaths := configuration.Http().StaticExcludedPaths()
    if nil == excludedPaths {
        t.Fatalf("expected an empty list rather than nil")
    }

    if 0 != len(excludedPaths) {
        t.Fatalf("expected the default to exclude nothing, got %v", excludedPaths)
    }
}

func TestConfigurationHttpStaticExcludedPathsAreReadAsACommaSeparatedList(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            StaticExcludedPathsKey: "/admin, /api/internal ,/downloads",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    excludedPaths := configuration.Http().StaticExcludedPaths()

    expected := []string{"/admin", "/api/internal", "/downloads"}
    if len(expected) != len(excludedPaths) {
        t.Fatalf("expected %d entries, got %v", len(expected), excludedPaths)
    }

    for index, value := range expected {
        if value != excludedPaths[index] {
            t.Fatalf("expected entry %d to be %q, got %q", index, value, excludedPaths[index])
        }
    }
}

func TestConfigurationHttpStaticExcludedPathsAreReadThroughTheKernelAlias(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            StaticExcludedPathsKey: "/admin",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if "/admin" != configuration.MustGet(KernelStaticExcludedPaths).MustString() {
        t.Fatalf("expected the kernel alias to carry the value, got %q", configuration.MustGet(KernelStaticExcludedPaths).MustString())
    }
}

func TestConfigurationHttpStaticExcludedPathsAreCopiedOnRead(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            StaticExcludedPathsKey: "/admin",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    excludedPaths := configuration.Http().StaticExcludedPaths()
    if 1 != len(excludedPaths) {
        t.Fatalf("expected one entry, got %v", excludedPaths)
    }

    excludedPaths[0] = "/"

    if "/admin" != configuration.Http().StaticExcludedPaths()[0] {
        t.Fatalf("writing into the returned list reached the configuration, got %q", configuration.Http().StaticExcludedPaths()[0])
    }
}

func TestConfigurationHttpStaticExcludedPathsRefuseAnEntryThatIsNotAPath(t *testing.T) {
    /* an entry that cannot be a prefix of a request path would exclude nothing while the application believes the directory is hers, and an empty entry would exclude everything; both are refused at boot rather than guessed at. */
    for _, value := range []string{"admin", "/admin,", ",", "/admin, ,/api"} {
        source := &testEnvironmentSource{
            values: map[string]string{
                StaticExcludedPathsKey: value,
            },
        }

        environment, err := NewEnvironment(source)
        if nil != err {
            t.Fatalf("new environment error: %v", err)
        }

        _, err = NewConfiguration(environment, "/tmp/melody")
        if nil == err {
            t.Fatalf("expected the excluded path list %q to fail the boot", value)
        }
    }
}

func TestConfigurationHttpStaticExcludedPathsAcceptAWhitespaceOnlyValueAsNoList(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            StaticExcludedPathsKey: "   ",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("expected a blank value to read as no list, got %v", err)
    }

    if 0 != len(configuration.Http().StaticExcludedPaths()) {
        t.Fatalf("expected a blank value to exclude nothing, got %v", configuration.Http().StaticExcludedPaths())
    }
}

func TestNewHttpConfiguration_RefusesEveryValueItCannotAct(t *testing.T) {
    cases := []struct {
        name                string
        address             string
        defaultLocale       string
        publicDir           string
        staticIndexFile     string
        staticExcludedPaths []string
        expectedMessage     string
    }{
        {
            name:            "an address with no port at all",
            address:         ":",
            expectedMessage: "http port is invalid",
        },
        {
            name:            "an address whose port is not a number",
            address:         "localhost:http",
            expectedMessage: "http port is invalid",
        },
        {
            name:            "a port above the range",
            address:         ":65536",
            expectedMessage: "http port is out of range",
        },
        {
            name:            "a port below the range",
            address:         ":0",
            expectedMessage: "http port is out of range",
        },
        {
            name:            "an address that is not host and port",
            address:         "localhost:8080:9090",
            expectedMessage: "http address is invalid",
        },
        {
            name:            "an empty locale",
            address:         ":8080",
            defaultLocale:   "",
            expectedMessage: "default locale may not be empty",
        },
        {
            name:            "a locale that is not a language tag",
            address:         ":8080",
            defaultLocale:   "english",
            expectedMessage: "default locale is invalid",
        },
        {
            name:            "an empty public directory",
            address:         ":8080",
            defaultLocale:   "en",
            publicDir:       "",
            expectedMessage: "public directory may not be empty",
        },
        {
            name:            "a public directory that climbs out of the project",
            address:         ":8080",
            defaultLocale:   "en",
            publicDir:       "public/../..",
            expectedMessage: "public directory is invalid",
        },
        {
            name:            "an empty index file",
            address:         ":8080",
            defaultLocale:   "en",
            publicDir:       "public",
            staticIndexFile: "",
            expectedMessage: "static index file may not be empty",
        },
        {
            name:            "an index file that carries a path",
            address:         ":8080",
            defaultLocale:   "en",
            publicDir:       "public",
            staticIndexFile: "html/index.html",
            expectedMessage: "static index file is invalid",
        },
        {
            name:                "a blank address",
            address:             "   ",
            defaultLocale:       "en",
            publicDir:           "public",
            staticIndexFile:     "index.html",
            expectedMessage:     "http address may not be empty",
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            httpConfigurationInstance, httpErr := newHttpConfiguration(
                testCase.address,
                testCase.defaultLocale,
                testCase.publicDir,
                testCase.staticIndexFile,
                bound.Resolved[int]{Value: 1024},
                false,
                bound.Resolved[int]{Value: 3600},
                testCase.staticExcludedPaths,
                bound.Resolved[time.Duration]{Value: DefaultSessionTtl},
                bound.Resolved[time.Duration]{Value: DefaultSessionTombstoneRetention},
                bound.Resolved[time.Duration]{Value: DefaultHttpShutdownTimeout},
                httpServerLimits{},
            )

            if nil == httpErr {
                t.Fatalf("expected the value to be refused")
            }

            if nil != httpConfigurationInstance {
                t.Fatalf("expected no configuration over a refused value")
            }

            if testCase.expectedMessage != httpErr.Error() {
                t.Fatalf("expected %q, got %q", testCase.expectedMessage, httpErr.Error())
            }
        })
    }
}

func TestNewHttpConfiguration_CompletesABarePortAndKeepsEveryAcceptedValue(t *testing.T) {
    httpConfigurationInstance, httpErr := newHttpConfiguration(
        "8080",
        "en-GB",
        "public",
        "index.html",
        bound.Resolved[int]{Value: 2048},
        true,
        bound.Resolved[int]{Value: 3600},
        []string{"/internal"},
        bound.Resolved[time.Duration]{Value: time.Minute},
        bound.Resolved[time.Duration]{Value: 11 * time.Minute},
        bound.Resolved[time.Duration]{Value: 7 * time.Second},
        httpServerLimits{readTimeout: bound.Of(time.Second)},
    )
    if nil != httpErr {
        t.Fatalf("unexpected error: %v", httpErr)
    }

    if ":8080" != httpConfigurationInstance.Address() {
        t.Fatalf("expected a bare port to be completed, got %q", httpConfigurationInstance.Address())
    }
    if "en-GB" != httpConfigurationInstance.DefaultLocale() {
        t.Fatalf("unexpected locale: %q", httpConfigurationInstance.DefaultLocale())
    }
    if "public" != httpConfigurationInstance.PublicDir() {
        t.Fatalf("unexpected public directory: %q", httpConfigurationInstance.PublicDir())
    }
    if "index.html" != httpConfigurationInstance.StaticIndexFile() {
        t.Fatalf("unexpected index file: %q", httpConfigurationInstance.StaticIndexFile())
    }
    if 2048 != httpConfigurationInstance.MaxRequestBodyBytes().Value {
        t.Fatalf("unexpected body limit: %+v", httpConfigurationInstance.MaxRequestBodyBytes())
    }
    if false == httpConfigurationInstance.StaticEnableCache() {
        t.Fatalf("expected the static cache to be enabled")
    }
    if 3600 != httpConfigurationInstance.StaticCacheMaxAge().Value {
        t.Fatalf("unexpected cache max age: %+v", httpConfigurationInstance.StaticCacheMaxAge())
    }
    if time.Minute != httpConfigurationInstance.SessionTtl().Value {
        t.Fatalf("unexpected session ttl: %+v", httpConfigurationInstance.SessionTtl())
    }
    if 11*time.Minute != httpConfigurationInstance.SessionTombstoneRetention().Value {
        t.Fatalf("unexpected session tombstone retention: %+v", httpConfigurationInstance.SessionTombstoneRetention())
    }
    if 7*time.Second != httpConfigurationInstance.ShutdownTimeout().Value {
        t.Fatalf("unexpected shutdown timeout: %+v", httpConfigurationInstance.ShutdownTimeout())
    }
    if bound.Of(time.Second) != httpConfigurationInstance.ReadTimeout() {
        t.Fatalf("unexpected read timeout: %v", httpConfigurationInstance.ReadTimeout())
    }
}
