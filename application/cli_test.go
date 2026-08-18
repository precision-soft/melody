package application

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/internal/testhelper"
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

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid mode: --mode is a runtime flag")
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

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid role: --role is a runtime flag")
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

func TestStripRuntimeFlagsFromOsArgs_ConsumesARealFlagValue(t *testing.T) {
    originalArguments := os.Args
    defer func() { os.Args = originalArguments }()

    os.Args = []string{"melody-example", "--role", "worker", "app:info"}
    stripRuntimeFlagsFromOsArgs()

    if 2 != len(os.Args) || "app:info" != os.Args[1] {
        t.Fatalf("expected the runtime flag and its value to be stripped, got %v", os.Args)
    }
}

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

func TestParseRuntimeFlags_PanicsOnExplicitlyEmptyRole(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role="}

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid role: --role is a runtime flag")
}

func TestParseRuntimeFlags_PanicsOnBareRoleWithoutValue(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "--mode=http"}

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid role: --role is a runtime flag")
}

func TestParseRuntimeFlags_PanicsOnExplicitlyEmptyMode(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--mode="}

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid mode: --mode is a runtime flag")
}

func TestParseRuntimeFlags_LastRoleOccurrenceWins(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role", "web", "--role", "worker", "someCommand"}

    flags := ParseRuntimeFlags(config.ModeHttp)
    if config.RoleWorker != flags.Role() {
        t.Fatalf("expected role %q, got %q", config.RoleWorker, flags.Role())
    }
}

func TestParseRuntimeFlags_PanicsWhenLastRoleOccurrenceIsEmpty(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role=web", "--role=", "someCommand"}

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid role: --role is a runtime flag")
}

func TestParseRuntimeFlags_BareRoleBeforeCommandConsumesIt(t *testing.T) {
    originalArguments := os.Args
    t.Cleanup(func() {
        os.Args = originalArguments
    })

    os.Args = []string{"app", "--role=web", "--role", "someCommand"}

    testhelper.AssertPanicsWithError(t, func() {
        _ = ParseRuntimeFlags(config.ModeHttp)
    }, "invalid role: --role is a runtime flag")
}

func TestNewRuntimeFlagsWithRole_AnEmptyRoleWidensToAll(t *testing.T) {
    if config.RoleAll != NewRuntimeFlagsWithRole(config.ModeCli, "").Role() {
        t.Fatalf("expected an empty role to widen to all, got %q", NewRuntimeFlagsWithRole(config.ModeCli, "").Role())
    }

    if config.RoleWorker != NewRuntimeFlagsWithRole(config.ModeCli, config.RoleWorker).Role() {
        t.Fatalf("expected an explicit role to be kept")
    }

    if config.ModeCli != NewRuntimeFlagsWithRole(config.ModeCli, config.RoleWorker).Mode() {
        t.Fatalf("expected the mode to be kept")
    }
}
