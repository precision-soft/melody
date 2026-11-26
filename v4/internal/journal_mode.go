package internal

import (
    "os"
)

const (
    JournalFileModeFloor      os.FileMode = 0o600
    JournalDirectoryModeFloor os.FileMode = 0o700

    JournalFileModeRefusal      = "the journal file mode must be an octal permission that lets the owner read and write"
    JournalDirectoryModeRefusal = "the journal directory mode must be an octal permission that lets the owner read, write and search"
)

/* IsJournalMode is the one rule of a journal mode: permission bits only, every bit of the floor kept */
func IsJournalMode(mode os.FileMode, floor os.FileMode) bool {
    if 0 != mode&^os.ModePerm {
        return false
    }

    return floor == mode&floor
}
