package config

import (
    "testing"
)

func TestModuleParameterValue(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        environmentKeyRedisAddress:     "redis:6379",
        environmentKeySmtpPassword:     "pa%%ss",
        environmentKeyCorsAllowOrigins: "a%%b%%c",
    })

    cases := []struct {
        parameter string
        expected  string
    }{
        {parameter: parameterRedisAddress, expected: "redis:6379"},
        {parameter: parameterSmtpPassword, expected: "pa%ss"},
        {parameter: parameterCorsAllowOrigins, expected: "a%b%c"},
        {parameter: parameterAmqpDsn, expected: ""},
    }

    for _, testCase := range cases {
        if got := moduleInstance.parameterValue(testCase.parameter); testCase.expected != got {
            t.Fatalf("parameterValue(%q): wanted %q, got %q", testCase.parameter, testCase.expected, got)
        }
    }
}
