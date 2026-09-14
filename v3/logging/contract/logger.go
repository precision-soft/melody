package contract

type Context map[string]any

type Logger interface {
    Log(level Level, message string, context Context)

    Debug(message string, context Context)

    Info(message string, context Context)

    Warning(message string, context Context)

    Error(message string, context Context)

    Emergency(message string, context Context)
}

/* LevelReporter optionally lets callers avoid preparing disabled log records. Loggers without it are treated as enabled. Enabled concerns only the level; callers must still perform work whose results are used outside logging. */
type LevelReporter interface {
    Enabled(level Level) bool
}
