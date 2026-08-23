package config

import (
    "testing"
    "time"
)

func TestConfigurationHttpSessionTtlDefaultsToABoundedLifetime(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if 0 != configuration.Http().SessionTtl() {
        t.Fatalf("expected the default session ttl to stay unbounded, got %v", configuration.Http().SessionTtl())
    }
}

func TestConfigurationHttpSessionTtlKeepsZeroAsAnExplicitChoice(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpSessionTtlKey: "0",
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

    if 0 != configuration.Http().SessionTtl() {
        t.Fatalf("expected an explicit zero to stay unbounded, got %v", configuration.Http().SessionTtl())
    }
}

func TestConfigurationHttpShutdownTimeoutDefaultsToFiveSeconds(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if DefaultHttpShutdownTimeout != configuration.Http().ShutdownTimeout() {
        t.Fatalf("expected the default shutdown timeout, got %v", configuration.Http().ShutdownTimeout())
    }
}

func TestConfigurationHttpShutdownTimeoutIsReadFromTheEnvironment(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpShutdownTimeoutKey: "45s",
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

    if 45*time.Second != configuration.Http().ShutdownTimeout() {
        t.Fatalf("expected the configured shutdown timeout, got %v", configuration.Http().ShutdownTimeout())
    }
}

func TestConfigurationHttpShutdownTimeoutRejectsZeroAndNegative(t *testing.T) {
    for _, value := range []string{"0", "-1s"} {
        source := &testEnvironmentSource{
            values: map[string]string{
                HttpShutdownTimeoutKey: value,
            },
        }

        environment, err := NewEnvironment(source)
        if nil != err {
            t.Fatalf("new environment error: %v", err)
        }

        _, err = NewConfiguration(environment, "/tmp/melody")
        if nil == err {
            t.Fatalf("expected the shutdown timeout %q to fail the boot", value)
        }
    }
}

func TestConfigurationHttpShutdownTimeoutRejectsAnUnparsableValue(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpShutdownTimeoutKey: "soon",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    _, err = NewConfiguration(environment, "/tmp/melody")
    if nil == err {
        t.Fatalf("expected an unparsable shutdown timeout to fail the boot")
    }
}

func TestConfigurationHttpSessionTtlIsReadFromTheEnvironment(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpSessionTtlKey: "30m",
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

    if 30*time.Minute != configuration.Http().SessionTtl() {
        t.Fatalf("expected the configured session ttl, got %v", configuration.Http().SessionTtl())
    }
}

func TestConfigurationHttpSessionTtlRejectsANegativeValue(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpSessionTtlKey: "-1m",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    _, err = NewConfiguration(environment, "/tmp/melody")
    if nil == err {
        t.Fatalf("expected a negative session ttl to fail the boot")
    }
}

func TestConfigurationHttpSessionTtlRejectsAValueBelowTheFloor(t *testing.T) {
    cases := []string{"1ns", "1us", "500ms", "999ms"}

    for _, value := range cases {
        source := &testEnvironmentSource{
            values: map[string]string{
                HttpSessionTtlKey: value,
            },
        }

        environment, err := NewEnvironment(source)
        if nil != err {
            t.Fatalf("new environment error: %v", err)
        }

        _, err = NewConfiguration(environment, "/tmp/melody")
        if nil == err {
            t.Fatalf("expected the session ttl %q to fail the boot", value)
        }
    }
}

func TestConfigurationHttpSessionTtlAcceptsTheFloorAndZero(t *testing.T) {
    cases := map[string]time.Duration{
        "1s":  time.Second,
        "0":   0,
        "30m": 30 * time.Minute,
    }

    for value, expected := range cases {
        source := &testEnvironmentSource{
            values: map[string]string{
                HttpSessionTtlKey: value,
            },
        }

        environment, err := NewEnvironment(source)
        if nil != err {
            t.Fatalf("new environment error: %v", err)
        }

        configuration, err := NewConfiguration(environment, "/tmp/melody")
        if nil != err {
            t.Fatalf("expected the session ttl %q to be accepted, got %v", value, err)
        }

        if expected != configuration.Http().SessionTtl() {
            t.Fatalf("expected the session ttl %q to be read as %v, got %v", value, expected, configuration.Http().SessionTtl())
        }
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
        maxRequestBodyBytes int
        staticCacheMaxAge   int
        staticExcludedPaths []string
        sessionTtl          time.Duration
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
            name:                "an unbounded request body limit",
            address:             ":8080",
            defaultLocale:       "en",
            publicDir:           "public",
            staticIndexFile:     "index.html",
            maxRequestBodyBytes: 0,
            expectedMessage:     "invalid http max request body bytes",
        },
        {
            name:                "a negative cache max age",
            address:             ":8080",
            defaultLocale:       "en",
            publicDir:           "public",
            staticIndexFile:     "index.html",
            maxRequestBodyBytes: 1024,
            staticCacheMaxAge:   -1,
            expectedMessage:     "static cache max age must be zero or positive",
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            httpConfigurationInstance, httpErr := newHttpConfiguration(
                testCase.address,
                testCase.defaultLocale,
                testCase.publicDir,
                testCase.staticIndexFile,
                testCase.maxRequestBodyBytes,
                false,
                testCase.staticCacheMaxAge,
                testCase.staticExcludedPaths,
                testCase.sessionTtl,
                DefaultHttpShutdownTimeout,
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
        2048,
        true,
        3600,
        []string{"/internal"},
        time.Minute,
        7*time.Second,
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
    if 2048 != httpConfigurationInstance.MaxRequestBodyBytes() {
        t.Fatalf("unexpected body limit: %d", httpConfigurationInstance.MaxRequestBodyBytes())
    }
    if false == httpConfigurationInstance.StaticEnableCache() {
        t.Fatalf("expected the static cache to be enabled")
    }
    if 3600 != httpConfigurationInstance.StaticCacheMaxAge() {
        t.Fatalf("unexpected cache max age: %d", httpConfigurationInstance.StaticCacheMaxAge())
    }
    if time.Minute != httpConfigurationInstance.SessionTtl() {
        t.Fatalf("unexpected session ttl: %s", httpConfigurationInstance.SessionTtl())
    }
    if 7*time.Second != httpConfigurationInstance.ShutdownTimeout() {
        t.Fatalf("unexpected shutdown timeout: %s", httpConfigurationInstance.ShutdownTimeout())
    }
}

func TestNewHttpConfiguration_RefusesANonPositiveShutdownTimeout(t *testing.T) {
    for _, invalidTimeout := range []time.Duration{0, -time.Second} {
        httpConfigurationInstance, httpErr := newHttpConfiguration(
            ":8080",
            "en",
            "public",
            "index.html",
            1024,
            false,
            3600,
            nil,
            0,
            invalidTimeout,
        )

        if nil == httpErr {
            t.Fatalf("expected the shutdown timeout %v to be refused", invalidTimeout)
        }

        if nil != httpConfigurationInstance {
            t.Fatalf("expected no configuration over a refused value")
        }

        if "http shutdown timeout must be positive" != httpErr.Error() {
            t.Fatalf("expected the refusal to name the rule, got %q", httpErr.Error())
        }
    }
}
