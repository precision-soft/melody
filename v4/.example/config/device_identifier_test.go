package config

import (
    "strings"
    "testing"
)

func TestDeviceIdentifierBound_RefusesAValueOutsideOneTo255(t *testing.T) {
    for _, value := range []string{"0", "256", "-1", "sixty-four", "64.5"} {
        module := moduleWithEnvironment(t, map[string]string{environmentKeyDeviceIdentifierMaxBytes: value})

        refusal := recoveredRefusal(t, func() { module.deviceIdentifierMaximumBytes() })
        if nil == refusal || false == strings.Contains(refusal.Error(), "the device identifier bound is not an integer from 1 to 255") {
            t.Fatalf("expected %q refused at boot, got %v", value, refusal)
        }
    }
}

func TestDeviceIdentifierBound_TakesTheDefaultWhenEmptyAndTheValueWithinTheRange(t *testing.T) {
    expectations := map[string]int{"": 64, "1": 1, "255": 255, " 32 ": 32}

    for value, expected := range expectations {
        module := moduleWithEnvironment(t, map[string]string{environmentKeyDeviceIdentifierMaxBytes: value})

        if maximumBytes := module.deviceIdentifierMaximumBytes(); expected != maximumBytes {
            t.Fatalf("expected %q to bound at %d, got %d", value, expected, maximumBytes)
        }
    }
}
