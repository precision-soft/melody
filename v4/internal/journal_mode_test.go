package internal

import (
    "os"
    "testing"
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
