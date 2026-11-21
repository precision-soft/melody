/* Package kernelseam is the door the application installs its boot runtime on the kernel through; Token cannot be spelled outside v4, so no exported setter exists. */
package kernelseam

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Token struct{}

type BootRuntimeInstaller interface {
    InstallBootRuntime(token Token, bootRuntime containercontract.Runtime)
}
