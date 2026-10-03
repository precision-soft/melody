package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    bun "github.com/uptrace/bun"
)

const (
    /* secondFactorReplayGuardPrefix names the codes the sign-in and the verification door burn, so two applications on one redis never refuse each other's codes as replays */
    secondFactorReplayGuardPrefix = "melody-example-v3:2fa"

    /* secondFactorBudgetKeyPrefix names the per-account budget of presented second factors, apart from the per-address write budget */
    secondFactorBudgetKeyPrefix = "melody-example-v3:second_factor_budget:"

    /* secondFactorBudgetAllowance codes may be presented for one account inside secondFactorBudgetWindow: enough for a user who mistypes, far too few to walk a million six-digit codes */
    secondFactorBudgetAllowance = 5
    secondFactorBudgetWindow    = 15 * time.Minute
)

/* registerTwoFactorStoreService wires the two-factor store, which keeps a user's TOTP secret and single-use recovery codes encrypted at rest, only when a database is configured; the enrollment table belongs to the example's migration set. The store is resolved at the first request that needs it, so a refused migration answers 503 to that request and the next one resolves again. */
func (instance *Module) registerTwoFactorStoreService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.database {
        return
    }

    registrar.RegisterService(
        twofactor.ServiceStore,
        func(resolver melodycontainercontract.Resolver) (*twofactor.Store, error) {
            /* the handle is resolved by name, the way the catalogue storage resolves it, so the store closes ahead of the pool it reads through */
            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            if migrateErr := migration.EnsureMigrated(instance.processContext, database); nil != migrateErr {
                return nil, migrateErr
            }

            return twofactor.NewStore(database), nil
        },
    )
}

/* buildSecondFactorReplayGuard is the one memory of accepted codes both the sign-in and the verification door burn a code in, so a code spent at either door is refused at the other: in redis when the example has one, shared by every replica, and in this process otherwise. */
func (instance *Module) buildSecondFactorReplayGuard(clockInstance melodyclockcontract.Clock) melodysecuritycontract.NonceGuard {
    if nil == instance.redisClient {
        return melodysecurity.NewMemoryNonceGuardWithClock(clockInstance)
    }

    return melodyrueidis.NewNonceGuardWithPrefix(instance.redisClient, secondFactorReplayGuardPrefix)
}

/* buildLoginAuthentication is the chain the sign-in door authenticates through. Without a database there is no enrollment to read, so the password stands alone; with one, an enrolled account also presents a TOTP code or a recovery code, through the framework's second-factor authenticator, and the code is only read while the account's budget lasts. */
func (instance *Module) buildLoginAuthentication(
    clockInstance melodyclockcontract.Clock,
    replayGuard melodysecuritycontract.NonceGuard,
) *security.LoginAuthentication {
    password := security.NewPasswordAuthenticator(checkLoginPassword)

    if nil == instance.database {
        return security.NewLoginAuthentication(melodysecurity.NewAuthenticatorManager(password), nil)
    }

    budget := security.NewSecondFactorBudget(
        password,
        instance.buildSecondFactorBudgetLimiter(clockInstance),
        melodysecurity.DefaultTotpCodeHeaderName,
        melodysecurity.DefaultTotpRecoveryHeaderName,
    )

    secondFactor := melodysecurity.NewTotpSecondFactorAuthenticator(
        melodysecurity.TotpSecondFactorAuthenticatorConfig{
            Primary:     budget,
            Enrollments: twofactor.NewEnrollments(twofactor.StoreFromRuntime),
            ReplayGuard: replayGuard,
            Clock:       clockInstance,
        },
    )

    return security.NewLoginAuthentication(melodysecurity.NewAuthenticatorManager(secondFactor), budget)
}

/* buildSecondFactorBudgetLimiter counts the presented codes in redis when the example has one, so every replica spends one budget, and in this process otherwise; with redis unreachable the limiter fails closed, so a code is refused rather than read uncounted. */
func (instance *Module) buildSecondFactorBudgetLimiter(clockInstance melodyclockcontract.Clock) melodyhttpcontract.RateLimiter {
    if nil != instance.redisClient {
        return melodyrueidis.NewRateLimiter(
            instance.redisClient,
            secondFactorBudgetAllowance,
            secondFactorBudgetWindow,
            melodyrueidis.WithRateLimiterKeyPrefix(secondFactorBudgetKeyPrefix),
        )
    }

    limiter := melodyhttpmiddleware.NewSlidingWindowLimiterWithClock(clockInstance, secondFactorBudgetAllowance, secondFactorBudgetWindow)
    limiter.SetMaxKeys(inProcessWriteThrottleMaxAddresses)

    return limiter
}

/* checkLoginPassword checks a username and password against the accounts through the user service of the request's container. */
func checkLoginPassword(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
    return service.MustGetUserService(runtimeInstance.Container()).AuthenticateByUsernameAndPassword(runtimeInstance.Context(), username, password)
}
