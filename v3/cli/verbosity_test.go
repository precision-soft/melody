package cli

import (
    "testing"

    "github.com/precision-soft/melody/v3/cli/output"
)

/* the short verbosity flags are rewritten before the cli library parses anything, because the library has no notion of a repeated letter: -vv means level two here and nothing at all there. Everything else must pass through untouched — a lone dash, a long flag, a short flag that merely starts with v, and everything after the end-of-options terminator, which belongs to the command and not to the runtime. */
func TestNormalizeVerbosityArguments_RewritesOnlyTheRepeatedVerbosityFlag(t *testing.T) {
    cases := []struct {
        name      string
        arguments []string
        expected  []string
    }{
        {
            name:      "no arguments at all",
            arguments: []string{},
            expected:  []string{},
        },
        {
            name:      "a single -v becomes level one",
            arguments: []string{"app", "work", "-v"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=1"},
        },
        {
            name:      "the repeated letter becomes the level",
            arguments: []string{"app", "work", "-vvv"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=3"},
        },
        {
            name:      "a long flag is left to the library",
            arguments: []string{"app", "work", "--verbose"},
            expected:  []string{"app", "work", "--verbose"},
        },
        {
            name:      "a short flag that is not the verbosity one passes through",
            arguments: []string{"app", "work", "-x"},
            expected:  []string{"app", "work", "-x"},
        },
        {
            name:      "a short flag that merely starts with v passes through",
            arguments: []string{"app", "work", "-vx"},
            expected:  []string{"app", "work", "-vx"},
        },
        {
            name:      "a lone dash is not a flag",
            arguments: []string{"app", "work", "-"},
            expected:  []string{"app", "work", "-"},
        },
        {
            name:      "everything after the terminator belongs to the command",
            arguments: []string{"app", "work", "-vv", "--", "-vv", "-v"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=2", "--", "-vv", "-v"},
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            normalized := NormalizeVerbosityArguments(testCase.arguments)

            if len(testCase.expected) != len(normalized) {
                t.Fatalf("expected %d arguments, got %d: %#v", len(testCase.expected), len(normalized), normalized)
            }

            for index := range testCase.expected {
                if testCase.expected[index] != normalized[index] {
                    t.Fatalf("expected argument %d to be %q, got %q", index, testCase.expected[index], normalized[index])
                }
            }
        })
    }
}
