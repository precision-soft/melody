package config

import (
    "strings"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* the keys of the two signing secrets: the bearer tokens of the token firewall are verified with the first, and the envelopes of the internal firewall with the second, under the key id and the caller application beside it */
const (
    environmentKeyJwtSecret          = "APP_JWT_SECRET"
    environmentKeyInternalAuthKeyId  = "APP_INTERNAL_AUTH_KEY_ID"
    environmentKeyInternalAuthApp    = "APP_INTERNAL_AUTH_APP"
    environmentKeyInternalAuthSecret = "APP_INTERNAL_AUTH_SECRET"
)

/* isDevelopment answers whether the kernel runs in development, the one environment that accepts the credentials this example commits and seeds its accounts */
func (instance *Module) isDevelopment() bool {
    return melodyconfig.EnvDevelopment == instance.configuration.Kernel().Env()
}

/* credential answers the value a credential key holds. In development a blank key takes the value .env commits, so a checkout boots as it is; outside development a blank key or the committed value refuses the boot (see refuseDevelopmentCredential). */
func (instance *Module) credential(environmentKey string, developmentValue string) string {
    value := instance.environmentValue(environmentKey)

    if true == instance.isDevelopment() {
        if "" == strings.TrimSpace(value) {
            return developmentValue
        }

        return value
    }

    refuseDevelopmentCredential(environmentKey, value, []string{developmentValue}, nil)

    return value
}

/* refuseDevelopmentCredential panics the boot when a credential outside development is blank or is one of the values this example commits: those values are public, so a token signed, an envelope sealed or a column encrypted with them is open to anyone who read the repository. The refusal names the key and the context it is handed, never a value. */
func refuseDevelopmentCredential(
    environmentKey string,
    value string,
    developmentValueList []string,
    context melodyexceptioncontract.Context,
) {
    refusalContext := melodyexceptioncontract.Context{"environmentKey": environmentKey}
    for contextKey, contextValue := range context {
        refusalContext[contextKey] = contextValue
    }

    if "" == strings.TrimSpace(value) {
        melodyexception.Panic(melodyexception.NewError(
            environmentKey+" is required outside development",
            refusalContext,
            nil,
        ))
    }

    for _, developmentValue := range developmentValueList {
        if developmentValue == value {
            melodyexception.Panic(melodyexception.NewError(
                environmentKey+" holds the development value this example commits, which is public; outside development it needs a value of its own",
                refusalContext,
                nil,
            ))
        }
    }
}
