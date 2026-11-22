/* Package eventseam is the door the dispatcher tells a payload through that a listener runs after a failure; a Token is valid only when NewToken built it, which code outside v4 cannot call, and a payload refuses any other. */
package eventseam

type Token struct {
    valid bool
}

func NewToken() Token {
    return Token{valid: true}
}

/* Valid answers whether NewToken built the token; the zero Token, the only one code outside v4 can make, is not. */
func (instance Token) Valid() bool {
    return instance.valid
}

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
