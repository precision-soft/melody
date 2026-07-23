package config

import (
    "testing"

    melodyconfig "github.com/precision-soft/melody/v3/config"
)

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* @info environmentValue returns a .env-registered parameter fully resolved — NewConfiguration expands
%env(X)%/%name% indirection and unescapes %% at construction (before this composition root runs), so a
"pa%%ss" value reads back as "pa%ss" without environmentValue doing anything itself; a missing key returns ""
so an unset integration variable keeps its "skip this integration" behaviour. */
func TestModuleEnvironmentValue(t *testing.T) {
    source := &stubEnvironmentSource{values: map[string]string{
        "APP_PLAIN":   "redis:6379",
        "APP_PERCENT": "pa%%ss",
        "APP_MULTI":   "a%%b%%c",
    }}

    environment, environmentErr := melodyconfig.NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    moduleInstance := &Module{configuration: configuration}

    cases := []struct {
        key      string
        expected string
    }{
        {key: "APP_PLAIN", expected: "redis:6379"}, /* literal passes through unchanged */
        {key: "APP_PERCENT", expected: "pa%ss"},    /* %% collapses to a single % */
        {key: "APP_MULTI", expected: "a%b%c"},      /* every %% pair collapses */
        {key: "APP_MISSING", expected: ""},         /* absent key -> empty (skip-integration) */
    }

    for _, testCase := range cases {
        got := moduleInstance.environmentValue(testCase.key)
        if testCase.expected != got {
            t.Fatalf("environmentValue(%q): wanted %q, got %q", testCase.key, testCase.expected, got)
        }
    }
}
