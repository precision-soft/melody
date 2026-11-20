package contract

import "errors"

/* ErrBootFinished is the cause of the boot runtime's context once Boot returned: context.Cause answers it. */
var ErrBootFinished = errors.New("the application boot has finished")

/* ErrRuntimeKindRefused is the cause of a lookup or a door asked under a runtime of a kind that cannot answer it. */
var ErrRuntimeKindRefused = errors.New("the runtime's kind cannot answer the lookup")
