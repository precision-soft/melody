/* Package eventseam is the door the dispatcher tells a payload through that a listener runs after a failure; Token cannot be spelled outside v4, so no exported caller exists. */
package eventseam

type Token struct{}

/* AfterFailureListener is the listener of the after-failure pass the dispatcher is about to call. */
type AfterFailureListener struct {
    EventName        string
    ListenerName     string
    ReplacesResponse bool
}

/* AfterFailureGuard is implemented by a payload that treats a write made after a failure differently: Enter is called before each listener of the after-failure pass, and the function it answers after that listener returned, panicked or exited. */
type AfterFailureGuard interface {
    EnterAfterFailureListener(token Token, listener AfterFailureListener) (leave func())
}
