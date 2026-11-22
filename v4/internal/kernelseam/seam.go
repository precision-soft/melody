/* Package kernelseam is the door the application builds its kernel and installs the boot runtime on it through; Token cannot be spelled outside v4, so neither door is callable from userland. */
package kernelseam

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Token struct{}

type BootRuntimeInstaller interface {
    InstallBootRuntime(token Token, bootRuntime containercontract.Runtime)
}
