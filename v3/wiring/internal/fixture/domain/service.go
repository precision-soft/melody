package domain

import (
    "time"

    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/wiring/internal/fixture/domain/contract"
)

/* the types below stand in for an application domain: the scanner has to cover a constructor with no arguments, one mixing services with scalars, one that does not return an error, and the shapes it must decline to wire */

func NewUserRepository() *UserRepository {
    return &UserRepository{}
}

type UserRepository struct {
}

func NewUserService(
    repository *UserRepository,
    logger contract.Logger,
    sessionTtl time.Duration,
    stageEnv bool,
) (*UserService, error) {
    return &UserService{
        repository: repository,
        logger:     logger,
        sessionTtl: sessionTtl,
        stageEnv:   stageEnv,
    }, nil
}

type UserService struct {
    repository *UserRepository
    logger     contract.Logger
    sessionTtl time.Duration
    stageEnv   bool
}

func NewInvoiceService(repository *UserRepository, apiUrl string, retryCount int) *InvoiceService {
    return &InvoiceService{
        repository: repository,
        apiUrl:     apiUrl,
        retryCount: retryCount,
    }
}

type InvoiceService struct {
    repository *UserRepository
    apiUrl     string
    retryCount int
}

//melody:bind reportingUrl=fixture.reporting_url
func NewReportingService(reportingUrl string) *ReportingService {
    return &ReportingService{reportingUrl: reportingUrl}
}

type ReportingService struct {
    reportingUrl string
}

//melody:ignore
func NewExcludedByDirective(unbindable string) *ExcludedByDirective {
    return &ExcludedByDirective{}
}

type ExcludedByDirective struct {
}

func NewUserFixture() *UserFixture {
    return &UserFixture{}
}

type UserFixture struct {
}

func NewGenericHolder[T any](value T) *GenericHolder[T] {
    return &GenericHolder[T]{value: value}
}

type GenericHolder[T any] struct {
    value T
}

func NewVariadicService(parts ...string) *VariadicService {
    return &VariadicService{parts: parts}
}

type VariadicService struct {
    parts []string
}

func NewAuditTrail(repository *UserRepository) (AuditTrail, error) {
    return AuditTrail{repository: repository}, nil
}

type AuditTrail struct {
    repository *UserRepository
}

func NewRetryPolicy(maxAttempts uint8) *RetryPolicy {
    return &RetryPolicy{maxAttempts: maxAttempts}
}

type RetryPolicy struct {
    maxAttempts uint8
}

func NewParameterEcho(parameter *config.Parameter) *ParameterEcho {
    return &ParameterEcho{parameter: parameter}
}

type ParameterEcho struct {
    parameter *config.Parameter
}

func NewMigrationRunner() error {
    return nil
}

const ServiceCollisionProbe = "domain.collisionProbe"

/* the argument names collide with the generated body's own identifiers on purpose: the package aliases (domain, contract), the configuration helper and the closure parameter must all survive a constructor written in idiomatic Go */
//melody:service ServiceCollisionProbe
func NewCollisionProbe(
    domain *UserRepository,
    contract contract.Logger,
    configuration *UserRepository,
    resolver *UserRepository,
    retryCount int,
    refreshBudget uint32,
) (*CollisionProbe, error) {
    return &CollisionProbe{
        domain:        domain,
        contract:      contract,
        configuration: configuration,
        resolver:      resolver,
        retryCount:    retryCount,
        refreshBudget: refreshBudget,
    }, nil
}

type CollisionProbe struct {
    domain        *UserRepository
    contract      contract.Logger
    configuration *UserRepository
    resolver      *UserRepository
    retryCount    int
    refreshBudget uint32
}

func NewToken() string {
    return "token"
}
