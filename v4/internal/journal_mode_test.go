package internal

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func TestIsJournalMode_AdmitsPermissionBitsKeepingTheFloor(t *testing.T) {
    for _, testCase := range []struct {
        mode     os.FileMode
        floor    os.FileMode
        admitted bool
    }{
        {mode: 0o640, floor: JournalFileModeFloor, admitted: true},
        {mode: 0o600, floor: JournalFileModeFloor, admitted: true},
        {mode: 0o777, floor: JournalFileModeFloor, admitted: true},
        {mode: 0o400, floor: JournalFileModeFloor, admitted: false},
        {mode: 0o200, floor: JournalFileModeFloor, admitted: false},
        {mode: 0o1640, floor: JournalFileModeFloor, admitted: false},
        {mode: os.ModeSetuid | 0o640, floor: JournalFileModeFloor, admitted: false},
        {mode: 0o750, floor: JournalDirectoryModeFloor, admitted: true},
        {mode: 0o700, floor: JournalDirectoryModeFloor, admitted: true},
        {mode: 0o600, floor: JournalDirectoryModeFloor, admitted: false},
        {mode: 0o500, floor: JournalDirectoryModeFloor, admitted: false},
        {mode: os.ModeDir | 0o750, floor: JournalDirectoryModeFloor, admitted: false},
    } {
        if testCase.admitted != IsJournalMode(testCase.mode, testCase.floor) {
            t.Fatalf("expected mode %o under floor %o admitted %v", testCase.mode, testCase.floor, testCase.admitted)
        }
    }
}

func TestResolveCreationMode_DefaultsZeroAndRefusesOutsideTheRuleNamingTheField(t *testing.T) {
    if mode, err := ResolveCreationMode(0, 0o640, JournalFileModeFloor, FileModeRefusal, "Config.FileMode"); nil != err || 0o640 != mode {
        t.Fatalf("expected the default 0640, got %o and %v", mode, err)
    }

    if mode, err := ResolveCreationMode(0o660, 0o640, JournalFileModeFloor, FileModeRefusal, "Config.FileMode"); nil != err || 0o660 != mode {
        t.Fatalf("expected 0660 admitted, got %o and %v", mode, err)
    }

    _, err := ResolveCreationMode(0o650, 0o750, JournalDirectoryModeFloor, DirectoryModeRefusal, "Config.DirectoryMode")
    refusal, isRefusal := err.(*exception.Error)
    if false == isRefusal || DirectoryModeRefusal != refusal.Message() || "Config.DirectoryMode" != refusal.Context()["field"] || "0650" != refusal.Context()["value"] {
        t.Fatalf("expected the refusal naming the field and the value, got %v", err)
    }
}
