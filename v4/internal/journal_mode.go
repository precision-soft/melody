package internal

import (
    "fmt"
    "os"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

const (
    JournalFileModeFloor      os.FileMode = 0o600
    JournalDirectoryModeFloor os.FileMode = 0o700

    JournalFileModeRefusal      = "the journal file mode must be an octal permission that lets the owner read and write"
    JournalDirectoryModeRefusal = "the journal directory mode must be an octal permission that lets the owner read, write and search"

    FileModeRefusal      = "the file mode must be an octal permission that lets the owner read and write"
    DirectoryModeRefusal = "the directory mode must be an octal permission that lets the owner read, write and search"
)

/* IsJournalMode is the one rule of a journal mode: permission bits only, every bit of the floor kept */
func IsJournalMode(mode os.FileMode, floor os.FileMode) bool {
    if 0 != mode&^os.ModePerm {
        return false
    }

    return floor == mode&floor
}

/* ResolveCreationMode answers defaultMode for a zero mode and refuses a mode outside IsJournalMode naming the field and the value in octal */
func ResolveCreationMode(mode os.FileMode, defaultMode os.FileMode, floor os.FileMode, refusal string, field string) (os.FileMode, error) {
    if 0 == mode {
        return defaultMode, nil
    }

    if false == IsJournalMode(mode, floor) {
        return 0, exception.NewError(
            refusal,
            exceptioncontract.Context{
                "field": field,
                "value": fmt.Sprintf("%#o", uint32(mode)),
            },
            nil,
        )
    }

    return mode, nil
}
