/* Package kernelseam is the door the application builds its kernel and installs the boot runtime on it through; a Token is valid only when NewToken built it, which code outside v4 cannot call, and both doors refuse any other. */
package kernelseam

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

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

type BootRuntimeInstaller interface {
    InstallBootRuntime(token Token, bootRuntime containercontract.Runtime)
}
