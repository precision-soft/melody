package rueidis

import (
    "testing"
)

func TestNewConnectionParameters_StoresFields(t *testing.T) {
    connectionParameters := NewConnectionParameters("localhost:6379", "app-user", "super-secret")

    if "localhost:6379" != connectionParameters.Address {
        t.Fatalf("address: expected %q, got %q", "localhost:6379", connectionParameters.Address)
    }

    if "app-user" != connectionParameters.User {
        t.Fatalf("user: expected %q, got %q", "app-user", connectionParameters.User)
    }

    if "super-secret" != connectionParameters.Password {
        t.Fatalf("password: expected %q, got %q", "super-secret", connectionParameters.Password)
    }
}

func TestConnectionParameters_SafeContext_ElidesPassword(t *testing.T) {
    connectionParameters := NewConnectionParameters("localhost:6379", "app-user", "super-secret")

    safeContext := connectionParameters.SafeContext()

    if "localhost:6379" != safeContext["address"] {
        t.Fatalf("safe context must carry the address, got %v", safeContext["address"])
    }

    if "app-user" != safeContext["user"] {
        t.Fatalf("safe context must carry the user, got %v", safeContext["user"])
    }

    if _, found := safeContext["password"]; true == found {
        t.Fatalf("safe context must not contain a password key")
    }

    for key, value := range safeContext {
        if "super-secret" == value {
            t.Fatalf("safe context leaks the password under key %q", key)
        }
    }
}

func TestParseAddressList(t *testing.T) {
    testCases := []struct {
        name     string
        value    string
        expected []string
    }{
        {name: "empty", value: "", expected: nil},
        {name: "whitespace only", value: "   ", expected: nil},
        {name: "single address", value: "127.0.0.1:6379", expected: []string{"127.0.0.1:6379"}},
        {name: "multiple addresses trimmed", value: " 10.0.0.1:6379 , 10.0.0.2:6379 ", expected: []string{"10.0.0.1:6379", "10.0.0.2:6379"}},
        {name: "empty segments skipped", value: "a:1,,b:2,", expected: []string{"a:1", "b:2"}},
        {name: "separators only", value: ",,,", expected: []string{}},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            addresses := parseAddressList(testCase.value)

            if len(testCase.expected) != len(addresses) {
                t.Fatalf("expected %d addresses, got %d (%v)", len(testCase.expected), len(addresses), addresses)
            }

            for index, expected := range testCase.expected {
                if expected != addresses[index] {
                    t.Fatalf("address %d: expected %q, got %q", index, expected, addresses[index])
                }
            }
        })
    }
}
