package contract

import (
    "encoding/json"
    "testing"
)

/* @info the label is what every record says its level was, and the two constructors are the two shapes a configuration can carry: a name and a numeric code (syslog-style). Only the string shape had ever been rendered, so nothing said the numeric one renders as a number instead of as Go's default formatting of an int */
func TestLevelLabel_RendersBothConstructedShapes(t *testing.T) {
    if "warning" != LevelLabelFromString("warning").String() {
        t.Fatalf("unexpected string label: %s", LevelLabelFromString("warning").String())
    }

    if "4" != LevelLabelFromInt(4).String() {
        t.Fatalf("unexpected int label: %s", LevelLabelFromInt(4).String())
    }
}

/* @info the zero value is constructible outside both constructors — a LevelLabels map written as a literal with a missing entry hands one out — and it carries no value at all; the fallback renders it instead of leaving the record's level field empty */
func TestLevelLabel_ZeroValue_RendersThroughTheFallback(t *testing.T) {
    zeroValue := LevelLabel{}

    if "<nil>" != zeroValue.String() {
        t.Fatalf("expected the fallback rendering, got %q", zeroValue.String())
    }
}

/* @info the label travels into the record through the encoder, not through String, so the two must agree on the shape: a numeric label that marshalled as a quoted string would silently change the type of the level field for every consumer of the log */
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

/* @info a level with no configured label falls back to the level's own name: a record filed under a level a partial configuration forgot must still say which level it was, not carry an empty field */
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

/* @info a numeric label configured for a level is handed back as it is, not re-spelled as the level name: the two accepted shapes are checked separately, and only the string one had been entered */
func TestLabelFor_NumericLabel_IsKept(t *testing.T) {
    labels := LevelLabels{
        LevelError: LevelLabelFromInt(3),
    }

    if "3" != labels.LabelFor(LevelError).String() {
        t.Fatalf("expected the numeric label, got %q", labels.LabelFor(LevelError).String())
    }
}

/* @info a configured label whose value is neither of the two shapes the constructors produce — the zero value, written directly into the map — is discarded in favour of the level name: rendering it would put "<nil>" in the level field of every record at that level */
func TestLabelFor_LabelOfAnUnsupportedShape_FallsBackToTheLevelName(t *testing.T) {
    labels := LevelLabels{
        LevelEmergency: {},
    }

    if "emergency" != labels.LabelFor(LevelEmergency).String() {
        t.Fatalf("expected the level name, got %q", labels.LabelFor(LevelEmergency).String())
    }
}
