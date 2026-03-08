package contract

type Level string

const (
    /** @internal */
    LevelUnknown Level = "unknown"

    LevelDebug     Level = "debug"
    LevelInfo      Level = "info"
    LevelWarning   Level = "warning"
    LevelError     Level = "error"
    LevelEmergency Level = "emergency"
)
