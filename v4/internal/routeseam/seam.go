/* Package routeseam is the door the application installs its boot collision recorder on the route registry through; a Token is valid only when NewToken built it, which code outside v4 cannot call, and the door refuses any other. */
package routeseam

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

type BootCollisionRecorderInstaller interface {
    InstallBootCollisionRecorder(token Token, recorder func(kind string, name string))
}
