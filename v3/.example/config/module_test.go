package config

import (
    "testing"
    melodyconfig "github.com/precision-soft/melody/v3/config"
)

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
        {key: "APP_PLAIN", expected: "redis:6379"},
        {key: "APP_PERCENT", expected: "pa%ss"},
        {key: "APP_MULTI", expected: "a%b%c"},
        {key: "APP_MISSING", expected: ""},
    }

    for _, testCase := range cases {
        got := moduleInstance.environmentValue(testCase.key)
        if testCase.expected != got {
            t.Fatalf("environmentValue(%q): wanted %q, got %q", testCase.key, testCase.expected, got)
        }
    }
}
