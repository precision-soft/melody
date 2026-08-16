package application

import (
    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
)

/* SecurityModule lives in application/contract beside its eight sibling hooks; the alias keeps every implementation and assertion written against the application package compiling. */
type SecurityModule = applicationcontract.SecurityModule
