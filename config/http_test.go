package config

import (
    "testing"
    "time"
)

/* @info an application that never names a session ttl still gets a bounded one, because melody mints a session for every cookie-less request and the default storage keeps whatever it is handed for the life of the process */
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

/* @info no expiry stays reachable, but only by asking for it: the value zero keeps its meaning and is not what an application gets by staying silent */
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

/* @info Validation rejected only a negative ttl, so "1ns" parsed and was accepted. FileStorage.Save purges every lapsed entry on the very write that stores the new one — the boundary measured between 1µs and 10µs — so the caller was told the save succeeded and nothing was persisted: a login that reports success and stores no session. */
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

/* @info The floor is one second, and one second itself is accepted; zero keeps its own meaning of no expiry. */
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
