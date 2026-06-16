package application

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/internal/testhelper"
)

func TestParseModeFlagValue(t *testing.T) {
    value, matched, consumeNext := parseModeFlagValue("-mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("-mode=cli")
    if false == matched || true == consumeNext || "cli" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--mode=http")
    if false == matched || true == consumeNext || "http" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseModeFlagValue("--other")
    if true == matched || true == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }
}

func TestHasNonRuntimeFlagArguments(t *testing.T) {
    if false == hasNonRuntimeFlagArguments([]string{"app"}) {
    } else {
        t.Fatalf("expected false")
    }

    if false == hasNonRuntimeFlagArguments([]string{"app", "-mode", "http"}) {
    } else {
        t.Fatalf("expected false")
    }

    if true == hasNonRuntimeFlagArguments([]string{"app", "serve"}) {
    } else {
        t.Fatalf("expected true")
    }

    if true == hasNonRuntimeFlagArguments([]string{"app", "-mode", "http", "serve"}) {
    } else {
        t.Fatalf("expected true")
    }
}

func TestStripRuntimeFlagsFromOsArgs(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "-mode", "http", "serve", "--mode=cli", "other"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "serve", "other"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %d args, got %d: %+v", len(expected), len(os.Args), os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected arg %d to be %q, got %q", index, expected[index], os.Args[index])
        }
    }
}

func TestParseRuntimeFlags_DefaultModeUsedWhenNoArgs(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_CliInferredWhenNonFlagArgsPresent(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "someCommand"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeCli, flags.Mode())
    }
}

func TestParseRuntimeFlags_ExplicitModeConsumesNextValue(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode", "cli"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeCli, flags.Mode())
    }
}

func TestParseRuntimeFlags_ExplicitModeSupportsEqualsSyntax(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode=http"}

    flags := ParseRuntimeFlags(config.ModeCli)
    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_PanicsOnInvalidMode(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode", "invalid"}

    testhelper.AssertPanics(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    })
}
