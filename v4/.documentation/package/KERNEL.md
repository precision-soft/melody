# KERNEL

The [`kernel`](../../kernel) package provides Melody’s framework kernel: a framework-internal orchestration object that wires and exposes core runtime infrastructure (configuration, container, HTTP router/kernel, event dispatcher, clock) to other framework subsystems.

## Scope

This is an internal integration package. Userland code should not construct a kernel directly; it should integrate through the application and runtime APIs.

## Subpackages

- [`kernel/contract`](../../kernel/contract)  
  Public kernel contracts used across framework packages.

## Responsibilities

- Construct a kernel instance and validate constructor invariants:
    - [`NewKernel`](../../kernel/kernel.go)
- Expose core infrastructure to framework subsystems through the `kernelcontract.Kernel` interface:
    - configuration access
    - container access
    - HTTP router and HTTP kernel access
    - event dispatcher access
    - clock access

## Exported API

### Contracts (`kernel/contract`)

- [`type Kernel`](../../kernel/contract/kernel.go)
- Kernel HTTP event name constants:
    - [`EventKernelRequest`](../../kernel/contract/event.go)
    - [`EventKernelController`](../../kernel/contract/event.go)
    - [`EventKernelResponse`](../../kernel/contract/event.go)
    - [`EventKernelTerminate`](../../kernel/contract/event.go)
    - [`EventKernelException`](../../kernel/contract/event.go)

### Constructors (`kernel`)

- [`NewKernel(...) kernelcontract.Kernel`](../../kernel/kernel.go)
