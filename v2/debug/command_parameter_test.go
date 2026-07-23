package debug

import (
    "strings"
    "testing"
)

/* @info the command exists to tell an operator whether a parameter carries a value, so redaction has to keep that answer while withholding the credential itself */
func TestRedactedParameterValue_HidesASecretButStillReportsWhetherItIsSet(t *testing.T) {
    cases := []struct {
        name     string
        value    any
        isSecret bool
        expected string
    }{
        {"secretWithValue", "P4ssPhrase", true, redactedValuePlaceholder},
        {"secretWithoutValue", "", true, redactedEmptyPlaceholder},
        {"ordinaryValue", "https://api.example.test", false, "https://api.example.test"},
        {"ordinaryEmptyValue", "", false, ""},
        {"secretNonStringValue", 12345, true, redactedValuePlaceholder},
        {"ordinaryNonStringValue", 12345, false, "12345"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            redacted := redactedParameterValue(testCase.value, testCase.isSecret)

            if testCase.expected != redacted {
                t.Fatalf("expected %q, got %q", testCase.expected, redacted)
            }
        })
    }
}

/* @info the redaction must not leak the length either: on a short credential it narrows the search meaningfully */
func TestRedactedParameterValue_DoesNotLeakTheSecretOrItsLength(t *testing.T) {
    secretValue := "P4ssPhrase"

    redacted := redactedParameterValue(secretValue, true)

    if true == strings.Contains(redacted, secretValue) {
        t.Fatalf("the redacted value leaked the credential")
    }

    if len(secretValue) == len(redacted) {
        t.Fatalf("the redacted value leaked the credential length")
    }

    if redacted != redactedParameterValue("a", true) {
        t.Fatalf("the redacted value varies with the credential length")
    }
}
