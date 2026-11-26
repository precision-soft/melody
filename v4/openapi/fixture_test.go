package openapi

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

type createProductRequest struct {
    Name  string  `json:"name" validate:"notBlank,min=2"`
    Email string  `json:"email" validate:"email"`
    Price float64 `json:"price"`
}

type productResponse struct {
    Id   string `json:"id"`
    Name string `json:"name"`
}

func testValidationRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}
