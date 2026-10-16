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

/* LevelReporter is the optional capability a Logger implements to say whether a record at a level would survive its threshold, for callers that build what they log. It stays outside Logger so every existing implementation still compiles; an absent LevelReporter means "enabled". Enabled answers for the level alone and is no permission to skip work something else reads. */
type LevelReporter interface {
    Enabled(level Level) bool
}
