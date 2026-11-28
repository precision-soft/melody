package contract

type Context map[string]any

type Logger interface {
    Log(level Level, message string, context Context)

    Debug(message string, context Context)

    Info(message string, context Context)

    Warning(message string, context Context)

    Error(message string, context Context)

    Emergency(message string, context Context)

    /* Enabled answers whether a record at the level would survive the logger's threshold, for callers that build what they log. It answers for the level alone and is no permission to skip work something else reads. */
    Enabled(level Level) bool

    /* CauseChainDepth is how many links of an error's cause chain LogError writes through this logger. */
    CauseChainDepth() int
}
