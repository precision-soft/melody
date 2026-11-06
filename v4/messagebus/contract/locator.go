package contract

type HandlerLocator interface {
    HandlersFor(message any) []MessageHandler
}
