package contract

import (
    "encoding/json"
    "testing"
)

func TestLevelLabel_RendersBothConstructedShapes(t *testing.T) {
    if "warning" != LevelLabelFromString("warning").String() {
        t.Fatalf("unexpected string label: %s", LevelLabelFromString("warning").String())
    }

    if "4" != LevelLabelFromInt(4).String() {
        t.Fatalf("unexpected int label: %s", LevelLabelFromInt(4).String())
    }
}

func TestLevelLabel_ZeroValue_RendersThroughTheFallback(t *testing.T) {
    zeroValue := LevelLabel{}

    if "<nil>" != zeroValue.String() {
        t.Fatalf("expected the fallback rendering, got %q", zeroValue.String())
    }
}

func TestLevelLabel_MarshalsAsItsUnderlyingShape(t *testing.T) {
    encodedString, err := json.Marshal(LevelLabelFromString("warning"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if `"warning"` != string(encodedString) {
        t.Fatalf("unexpected json for a string label: %s", string(encodedString))
    }

    encodedInt, err := json.Marshal(LevelLabelFromInt(4))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "4" != string(encodedInt) {
        t.Fatalf("unexpected json for an int label: %s", string(encodedInt))
    }
}

func TestDefaultLevelLabels_NamesEveryKnownLevel(t *testing.T) {
    labels := DefaultLevelLabels()

    expectedList := map[Level]string{
        LevelDebug:     "debug",
        LevelInfo:      "info",
        LevelWarning:   "warning",
        LevelError:     "error",
        LevelEmergency: "emergency",
    }

    for level, expectedLabel := range expectedList {
        if expectedLabel != labels.LabelFor(level).String() {
            t.Fatalf("level %q: expected label %q, got %q", level, expectedLabel, labels.LabelFor(level).String())
        }
    }
}

func TestLabelFor_UnconfiguredLevel_FallsBackToTheLevelName(t *testing.T) {
    labels := LevelLabels{
        LevelError: LevelLabelFromString("configured-error"),
    }

    if "warning" != labels.LabelFor(LevelWarning).String() {
        t.Fatalf("expected the level name, got %q", labels.LabelFor(LevelWarning).String())
    }

    if "configured-error" != labels.LabelFor(LevelError).String() {
        t.Fatalf("expected the configured label, got %q", labels.LabelFor(LevelError).String())
    }
}

func TestLabelFor_NumericLabel_IsKept(t *testing.T) {
    labels := LevelLabels{
        LevelError: LevelLabelFromInt(3),
    }

    if "3" != labels.LabelFor(LevelError).String() {
        t.Fatalf("expected the numeric label, got %q", labels.LabelFor(LevelError).String())
    }
}

func TestLabelFor_LabelOfAnUnsupportedShape_FallsBackToTheLevelName(t *testing.T) {
    labels := LevelLabels{
        LevelEmergency: {},
    }

    if "emergency" != labels.LabelFor(LevelEmergency).String() {
        t.Fatalf("expected the level name, got %q", labels.LabelFor(LevelEmergency).String())
    }
}
