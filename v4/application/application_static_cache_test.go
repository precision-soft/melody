package application

import (
    "context"
    "slices"
    "testing"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func staticCacheMaxAgeWarningsOf(t *testing.T, mode string, values map[string]string, overrides map[string]string) []internal.BootWarning {
    t.Helper()

    applicationInstance := newEnvironmentKeyBootApplicationWithOverridesIn(t, values, nil, overrides, t.TempDir())
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: mode})
    applicationInstance.collectIgnoredStaticCacheMaxAge()

    warnings := make([]internal.BootWarning, 0)
    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameStaticCacheMaxAgeIgnored == bootWarning.Name {
            warnings = append(warnings, bootWarning)
        }
    }

    return warnings
}

/* a layered .env switches the cache off over a base that sets the max age: the switch wins and the max age is named once as ignored, a blank line or the default value included, since the line is what the operator wrote */
func TestBoot_TheDisabledStaticCacheWinsOverAMaxAgeSetBesideItWithABootWarning(t *testing.T) {
    for _, maxAge := range []string{"60", "3600", ""} {
        warnings := staticCacheMaxAgeWarningsOf(t, config.ModeHttp, map[string]string{config.StaticCacheDisabledKey: "true", config.StaticCacheMaxAgeKey: maxAge}, nil)
        if 1 != len(warnings) {
            t.Fatalf("max age %q: expected one warning, got %+v", maxAge, warnings)
        }

        environmentKeys, _ := warnings[0].Context["environmentKeys"].([]string)
        if false == slices.Equal([]string{config.StaticCacheDisabledKey, config.StaticCacheMaxAgeKey}, environmentKeys) || 1 != len(warnings[0].Context) {
            t.Fatalf("max age %q: expected both keys named and nothing else, got %v", maxAge, warnings[0].Context)
        }
    }
}

func TestBoot_TheIgnoredStaticCacheMaxAgeWarningNamesTheOverriddenParameter(t *testing.T) {
    warnings := staticCacheMaxAgeWarningsOf(t, config.ModeHttp, map[string]string{config.StaticCacheDisabledKey: "true"}, map[string]string{config.KernelStaticCacheMaxAge: "120"})
    if 1 != len(warnings) {
        t.Fatalf("expected one warning, got %+v", warnings)
    }

    environmentKeys, _ := warnings[0].Context["environmentKeys"].([]string)
    if false == slices.Equal([]string{config.StaticCacheDisabledKey}, environmentKeys) || config.KernelStaticCacheMaxAge != warnings[0].Context["parameterName"] || true != warnings[0].Context["overridden"] {
        t.Fatalf("expected the switch and the overridden parameter named, got %v", warnings[0].Context)
    }
}

func TestBoot_TheIgnoredStaticCacheMaxAgeWarningIsLeftOutWhenNothingIsIgnored(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        mode   string
        values map[string]string
    }{
        {"the switch alone", config.ModeHttp, map[string]string{config.StaticCacheDisabledKey: "true"}},
        {"a max age beside the enabled cache", config.ModeHttp, map[string]string{config.StaticCacheDisabledKey: "false", config.StaticCacheMaxAgeKey: "60"}},
        {"a command, which serves no file", config.ModeCli, map[string]string{config.StaticCacheDisabledKey: "true", config.StaticCacheMaxAgeKey: "60"}},
    } {
        if warnings := staticCacheMaxAgeWarningsOf(t, testCase.mode, testCase.values, nil); 0 != len(warnings) {
            t.Fatalf("%s: expected no warning, got %+v", testCase.name, warnings)
        }
    }
}

func TestBoot_AnHttpBootCollectsTheIgnoredStaticCacheMaxAge(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{
        EnvironmentFiles:   testhelper.NewEmbeddedEnvironmentFs(),
        PublicFiles:        testhelper.NewEmbeddedStaticFs(),
        ParameterOverrides: map[string]string{config.KernelStaticCacheDisabled: "true", config.KernelStaticCacheMaxAge: "60"},
    })
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})

    applicationInstance.Boot()

    for _, bootWarning := range applicationInstance.bootWarnings {
        if internal.BootWarningNameStaticCacheMaxAgeIgnored == bootWarning.Name {
            return
        }
    }

    t.Fatalf("expected the boot to collect the ignored max age, got %+v", applicationInstance.bootWarnings)
}
