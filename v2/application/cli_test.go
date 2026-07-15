package application

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/v2/config"
    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestParseRuntimeFlagValue(t *testing.T) {
    value, matched, consumeNext := parseRuntimeFlagValue("-mode", "mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseRuntimeFlagValue("--mode", "mode")
    if false == matched || false == consumeNext || "" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseRuntimeFlagValue("-mode=cli", "mode")
    if false == matched || true == consumeNext || "cli" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseRuntimeFlagValue("--mode=http", "mode")
    if false == matched || true == consumeNext || "http" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseRuntimeFlagValue("--role=worker", "role")
    if false == matched || true == consumeNext || "worker" != value {
        t.Fatalf("unexpected result: value=%q matched=%v consumeNext=%v", value, matched, consumeNext)
    }

    value, matched, consumeNext = parseRuntimeFlagValue("--other", "mode")
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

    /* the leading -mode is a runtime flag and is stripped; the --mode=cli that follows the subcommand is the command's own flag and is left intact */
    os.Args = []string{"app", "-mode", "http", "serve", "--mode=cli", "other"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "serve", "--mode=cli", "other"}
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

func TestParseRuntimeFlags_RoleDefaultsToAll(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.RoleAll != flags.Role() {
        t.Fatalf("expected role %q, got %q", config.RoleAll, flags.Role())
    }
}

func TestParseRuntimeFlags_RoleFlagWinsOverConfiguredDefault(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role=worker"}

    flags := ParseRuntimeFlagsWithRole(config.ModeHttp, config.RoleWeb)
    if config.RoleWorker != flags.Role() {
        t.Fatalf("expected role %q, got %q", config.RoleWorker, flags.Role())
    }
}

func TestParseRuntimeFlags_RoleFlagAloneDoesNotImplyCliMode(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    /* a lone --role must not flip the process into cli mode: the worker container runs the same image with only the role changed */
    os.Args = []string{"app", "--role=worker"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_RoleFlagConsumesNextValue(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "web"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.RoleWeb != flags.Role() {
        t.Fatalf("expected role %q, got %q", config.RoleWeb, flags.Role())
    }

    if config.ModeHttp != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeHttp, flags.Mode())
    }
}

func TestParseRuntimeFlags_PanicsOnInvalidRole(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role=manager"}

    testhelper.AssertPanics(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    })
}

func TestStripRuntimeFlagsFromOsArgs_StripsRoleFlags(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    /* the leading --role worker is a runtime flag and is stripped; the --role=web and -mode=cli that follow the subcommand are the command's own flags and are left intact */
    os.Args = []string{"app", "--role", "worker", "someCommand", "--role=web", "-mode=cli", "other"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "someCommand", "--role=web", "-mode=cli", "other"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %d args, got %d: %+v", len(expected), len(os.Args), os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected arg %d to be %q, got %q", index, expected[index], os.Args[index])
        }
    }
}

/** @info a --role that follows the subcommand is the command's own flag: it and everything after the command name is left intact, so the command receives its --role and its --verbose. */
func TestStripRuntimeFlagsFromOsArgs_LeavesTheCommandsOwnFlags(t *testing.T) {
    originalArguments := os.Args
    defer func() { os.Args = originalArguments }()

    os.Args = []string{"melody-example", "report:generate", "--role", "--verbose", "target"}
    stripRuntimeFlagsFromOsArgs()

    expected := []string{"melody-example", "report:generate", "--role", "--verbose", "target"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %v, got %v", expected, os.Args)
    }
    for index := range expected {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected %v, got %v", expected, os.Args)
        }
    }
}

/** @info a bare `--role worker` before the subcommand still consumes its value and both are stripped. */
func TestStripRuntimeFlagsFromOsArgs_ConsumesARealFlagValue(t *testing.T) {
    originalArguments := os.Args
    defer func() { os.Args = originalArguments }()

    os.Args = []string{"melody-example", "--role", "worker", "app:info"}
    stripRuntimeFlagsFromOsArgs()

    if 2 != len(os.Args) || "app:info" != os.Args[1] {
        t.Fatalf("expected the runtime flag and its value to be stripped, got %v", os.Args)
    }
}

/** @info a command that declares its own --role flag is no longer broken by the runtime: an --role after the subcommand name is left in os.Args for the command to parse. */
func TestStripRuntimeFlagsFromOsArgs_PreservesCommandOwnRoleFlag(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "create:user", "--role", "admin"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "create:user", "--role", "admin"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %+v, got %+v", expected, os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected %+v, got %+v", expected, os.Args)
        }
    }
}

/** @info only the runtime --role that precedes the subcommand is stripped; the command keeps its own --role after the command name. */
func TestStripRuntimeFlagsFromOsArgs_StripsOnlyTheLeadingRole(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "worker", "create:user", "--role", "admin"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "create:user", "--role", "admin"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %+v, got %+v", expected, os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected %+v, got %+v", expected, os.Args)
        }
    }
}

/** @info a command's own --role after the subcommand is not read as the process role, so an app value like "admin" no longer panics the runtime; the process role stays the default and the command's flag is left for the command. */
func TestParseRuntimeFlags_IgnoresRoleAfterTheSubcommand(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "create:user", "--role", "admin"}

    flags := ParseRuntimeFlagsWithRole(config.ModeHttp, config.RoleAll)

    if config.RoleAll != flags.Role() {
        t.Fatalf("expected the default role, got %q", flags.Role())
    }

    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected cli mode, got %q", flags.Mode())
    }
}

/** @info the runtime --role that precedes the subcommand still wins; the command's own --role after the command name is ignored by the runtime parser. */
func TestParseRuntimeFlags_UsesTheLeadingRoleNotTheCommandFlag(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "worker", "create:user", "--role", "admin"}

    flags := ParseRuntimeFlagsWithRole(config.ModeHttp, config.RoleAll)

    if config.RoleWorker != flags.Role() {
        t.Fatalf("expected the leading worker role, got %q", flags.Role())
    }
}

/** @info A bare "--" end-of-options terminator makes every following token a literal command argument, exactly as the cli verbosity normalizer treats it. Before the terminator was honored the parser read `--role=manager` past the terminator and the boot panicked on the invalid role, stealing the command's own positional argument. */
func TestParseRuntimeFlags_TerminatorStopsRoleParsing(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "someCommand", "--", "--role=manager"}

    flags := ParseRuntimeFlags(config.ModeHttp)

    if config.RoleAll != flags.Role() {
        t.Fatalf("expected role %q, got %q", config.RoleAll, flags.Role())
    }

    if config.ModeCli != flags.Mode() {
        t.Fatalf("expected mode %q, got %q", config.ModeCli, flags.Mode())
    }
}

/** @info Tokens after the bare "--" terminator are literal command arguments and must survive stripping intact; before the terminator was honored `--role=web` was deleted from os.Args and the command never received it. */
func TestStripRuntimeFlagsFromOsArgs_TerminatorKeepsLiteralArguments(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "someCommand", "--", "--role=web", "target"}

    stripRuntimeFlagsFromOsArgs()

    expected := []string{"app", "someCommand", "--", "--role=web", "target"}
    if len(expected) != len(os.Args) {
        t.Fatalf("expected %d args, got %d: %+v", len(expected), len(os.Args), os.Args)
    }

    for index := 0; index < len(expected); index++ {
        if expected[index] != os.Args[index] {
            t.Fatalf("expected arg %d to be %q, got %q", index, expected[index], os.Args[index])
        }
    }
}

/** @info An explicitly present but empty `--role=` (e.g. expanded from an unset env var) must fail closed like any invalid role instead of silently widening to the most permissive RoleAll. */
func TestParseRuntimeFlags_PanicsOnExplicitlyEmptyRole(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role="}

    testhelper.AssertPanics(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    })
}

/** @info A bare `--role` that cannot consume its dash-leading next token is still explicitly present with no value, so it must fail closed rather than silently resolve to RoleAll. */
func TestParseRuntimeFlags_PanicsOnBareRoleWithoutValue(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "--mode=http"}

    testhelper.AssertPanics(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    })
}
