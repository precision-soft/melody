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

/* LevelReporter is the optional capability a Logger implements to say, before being handed anything, whether a record at a given level would survive its threshold. It exists for the callers that BUILD what they log: the event dispatcher assembles a context map per dispatch and resolves a listener's name through reflection per listener per dispatch, and every one of those was discarded unread by a journal at error level, on the hottest path the framework has.

It stays beside Logger rather than inside it because a logger is the most implemented contract melody has — a test double, an integrator's adapter to their own journal — and a method added to Logger would refuse each of them at compilation for a capability that is an optimization. A logger that does not implement this is asked nothing and keeps answering every call, so the answer to an absent LevelReporter is always "enabled": the reporting behaviour is the floor, never the thing the capability turns off.

Enabled answers for the LEVEL alone. It is not permission to skip work whose absence changes what is recorded — a caller that computes a value the record needs must still compute it when the answer is false and something else reads it. */
type LevelReporter interface {
    Enabled(level Level) bool
}
