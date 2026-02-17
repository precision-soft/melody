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
