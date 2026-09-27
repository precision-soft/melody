package rueidis

import (
    "testing"
)

func TestEscapeRedisGlobMeta(t *testing.T) {
    cases := []struct {
        name     string
        input    string
        expected string
    }{
        {name: "default token prefix is glob-safe and unchanged", input: "{melody:token}:user:", expected: "{melody:token}:user:"},
        {name: "square brackets escaped", input: "{app[eu]}:user:", expected: `{app\[eu\]}:user:`},
        {name: "star and question mark escaped", input: "{a*b?c}:user:", expected: `{a\*b\?c}:user:`},
        {name: "backslash escaped", input: `{back\slash}:user:`, expected: `{back\\slash}:user:`},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := escapeRedisGlobMeta(testCase.input)
            if testCase.expected != result {
                t.Fatalf("escapeRedisGlobMeta(%q) = %q, want %q (an unescaped glob metacharacter in the token-store prefix makes PurgeExpired SCAN MATCH miss or over-match the per-user index)", testCase.input, result, testCase.expected)
            }
        })
    }
}
