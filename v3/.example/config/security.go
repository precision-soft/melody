package config

import (
    "errors"
    "regexp"
    "strings"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/route"
    "github.com/precision-soft/melody/v3/.example/security"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodyaccesscontrol "github.com/precision-soft/melody/v3/security/accesscontrol"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        /* the index file is the resource the root serves, so it carries the same public rule: MELODY_STATIC_INDEX_FILE makes "/" and "/index.html" two spellings of one page. A door that is one path is opened by an exact rule, which speaks for that spelling and nothing beneath it or beside it, so a route added later under a similar spelling is not public by accident */
        melodyaccesscontrol.NewExactRule("/", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewExactRule("/index.html", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewExactRule(route.LoginPagePattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewExactRule(route.LogoutPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewExactRule(route.RoutesPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/assets", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/favicon", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        /* public for the locales the greeting is served in and nothing else: an unlisted locale matches no route, so it falls to the catch-all rather than to a public not-found */
        melodyaccesscontrol.NewRegexRule(i18nGreetingPublicPattern(), melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),

        melodyaccesscontrol.NewExactRule("/health", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewExactRule(metricsPath, melodyaccesscontrol.RuleConfig{
            Attributes: []string{instance.metricsAccessAttribute()},
        }),
        melodyaccesscontrol.NewExactRule("/openapi.json", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        /* the platform check holds the distributed lock for its whole timeout and runs three object-storage operations per call, so it carries the reader requirement; public, an anonymous caller could spend the lock and the bucket's request budget with one GET */
        melodyaccesscontrol.NewRegexRule("^/platform/check", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        /* a dispatch sends one mail per call through a backend the process does not own, so it takes the write role */
        melodyaccesscontrol.NewRegexRule("^/messagebus/dispatch", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewExactRule("/encrypt/roundtrip", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        /* the enrollment door binds a second factor to an account and returns its secret, and the verification door reads the answer back; behind an authenticated role the account is the caller's own token, so nobody can bind a factor to another account */
        melodyaccesscontrol.NewRegexRule("^/twofactor", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        /* the outbox doors insert a caller-chosen row and make the process publish the pending rows, and the storage doors write and read any object of the bucket: writes to three backends, behind the write role. The status read shares the prefix and therefore the role, so a door added under it inherits the requirement rather than the catch-all. */
        melodyaccesscontrol.NewRegexRule("^/outbox", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewRegexRule("^/storage", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),

        /* publishing injects a frame into every stream open across the cluster, so it takes the write role; streaming carries the catalog writes, so it takes an authenticated reader, and the handler gates the topic on top */
        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsPublishPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsStreamPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),

        /* the websocket door bridges onto the catalog topic the SSE stream gates behind RoleEditor and has no per-topic gate of its own, so the route carries the topic's whole requirement */
        melodyaccesscontrol.NewRegexRule("^/ws", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),

        melodyaccesscontrol.NewSegmentPrefixRule(route.ProductsPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CategoriesPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        /* the currency writes are catalogue data, as the products are, so they carry the editor's requirement; they stand before the listing's rule, which would otherwise claim them for any user */
        melodyaccesscontrol.NewSegmentPrefixRule(route.CurrenciesApiCreatePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CurrenciesApiUpdatePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CurrenciesApiDeletePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CurrenciesPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        /* the archive of catalogue readings carries the requirement of the catalogue listings, stated here rather than inherited from the catch-all, which can move */
        melodyaccesscontrol.NewSegmentPrefixRule(route.ReportsPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.UsersPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleAdmin},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.AccessTokenRevokeUserPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleAdmin},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.AccessTokenPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.DevicePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.SecurePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewRegexRule("^/", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
    )

    roleHierarchy := melodysecurity.NewRoleHierarchy(
        map[string][]string{
            entity.RoleAdmin:  {entity.RoleEditor, entity.RoleUser},
            entity.RoleEditor: {entity.RoleUser},
        },
    )

    accessDecisionManager := melodysecurity.NewAccessDecisionManager(
        melodysecuritycontract.DecisionStrategyAffirmative,
        melodysecurity.NewRoleVoter(),
    )

    entryPoint := security.NewLoginRedirectEntryPoint(route.LoginPagePattern)
    accessDeniedHandler := security.NewDefaultAccessDeniedHandler()

    builder.SetGlobal(
        accessControl,
        roleHierarchy,
        accessDecisionManager,
        entryPoint,
        accessDeniedHandler,
    )

    override := melodysecurityconfig.NewFirewallOverrideConfiguration()

    /* internal-auth (HMAC) firewall: a stateless machine-to-machine firewall on /internal that verifies the signed envelope a caller service sends and authenticates the call as that service principal. It carries its own authorization and reads nothing of the global list (overrideOnly), so the whole policy of the machine door is here: its one rule claims every path under the prefix for the service role. */
    builder.AddStatelessFirewall(
        "internal",
        melodysecurity.NewPathPrefixMatcher(route.InternalPrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewHmacTokenSource(melodysecurity.HmacTokenSourceConfig{
            Secrets:         instance.hmacSecrets,
            Apps:            instance.hmacApps,
            MaxFutureExpiry: internalEnvelopeMaxFutureExpiry,
            NonceGuard:      instance.internalNonceGuard(),
        }),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithAccessControl(melodysecurity.NewAccessControl(
                melodyaccesscontrol.NewSegmentPrefixRule(route.InternalPrefix, melodyaccesscontrol.RuleConfig{
                    Attributes: []string{internalCallerRole},
                }),
            )).
            WithMergeStrategy(melodysecurityconfig.AccessControlMergeOverrideOnly).
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    /* the token firewall's bearer source is decorated with switch-user impersonation: an admin holding ROLE_ALLOWED_TO_SWITCH can act as another user by sending X-Switch-User, and the resulting token authorizes as the target while keeping the admin readable (and auditable) as the impersonator. */
    builder.AddStatelessFirewall(
        "token",
        melodysecurity.NewPathPrefixMatcher(route.SecurePrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewImpersonationTokenSource(melodysecurity.ImpersonationTokenSourceConfig{
            Inner:         melodysecurity.NewBearerTokenSourceWithEnricher(instance.tokenValidator, newScopeRoleEnricher()),
            Users:         instance.impersonatedUsers,
            RoleHierarchy: roleHierarchy,
        }),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    /* the device firewall honours a token only while the account it names exists, with that account's current roles (see deviceAccountEnricher) */
    builder.AddStatelessFirewall(
        "deviceToken",
        melodysecurity.NewPathPrefixMatcher(route.DevicePrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewBearerTokenSourceWithEnricher(instance.opaqueTokenValidator, newDeviceAccountEnricher(repositoryDeviceAccountLookup)),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    instance.registerMetricsFirewall(builder)

    builder.AddFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewResolverTokenSource(security.SessionTokenResolver(sessionUserLookup)),
        route.LoginPagePattern,
        route.LogoutPattern,
        security.NewSessionLoginHandler(sessionUserLookup, sessionIndexLookup),
        security.NewSessionLogoutHandler(sessionIndexLookup),
        override,
    )
}

var _ melodyapplication.SecurityModule = (*Module)(nil)

/* sessionUserLookup reads the account through the repository, under the request's context: the user service answers usernames from its cache, and the session's authority must not outlive a change the cache has not dropped yet. */
func sessionUserLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return nil, false, errors.New("the request carries no runtime to read the account through")
    }

    userRepository, resolveErr := melodycontainer.FromResolver[repository.UserRepository](runtimeInstance.Container(), repository.ServiceUserRepository)
    if nil != resolveErr {
        return nil, false, resolveErr
    }

    return userRepository.FindById(runtimeInstance.Context(), userId)
}

/* sessionIndexLookup resolves the index of the sessions each account holds, which the sign-in doors keep under its cap and the sign-out doors release */
func sessionIndexLookup(request melodyhttpcontract.Request) (security.SessionIndex, error) {
    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return nil, errors.New("the request carries no runtime to resolve the session index through")
    }

    return melodycontainer.FromResolver[repository.UserSessionRepository](runtimeInstance.Container(), repository.ServiceUserSessionRepository)
}

/* i18nGreetingPublicPattern opens the i18n prefix under each locale the greeting route serves, spelled from the route's own list so the rule and the route cannot drift apart */
func i18nGreetingPublicPattern() string {
    quotedLocaleList := make([]string, 0, len(route.I18nGreetingLocaleList()))
    for _, locale := range route.I18nGreetingLocaleList() {
        quotedLocaleList = append(quotedLocaleList, regexp.QuoteMeta(locale))
    }

    return "^/(" + strings.Join(quotedLocaleList, "|") + ")" + regexp.QuoteMeta(route.I18nPrefix) + "(/|$)"
}


const (
    metricsPath = "/metrics"

    /* metricsScraperRole is the one role the metrics credential carries: it reads the exposition and nothing else */
    metricsScraperRole = "ROLE_MONITOR"
)

/* metricsAccessAttribute is what /metrics requires: the scraper's role when a metrics token is configured, public otherwise */
func (instance *Module) metricsAccessAttribute() string {
    if "" == instance.metricsToken {
        return melodysecuritycontract.AttributePublicAccess
    }

    return metricsScraperRole
}

/* registerMetricsFirewall declares the stateless door the scraper authenticates on: prometheus 2.x presents a credential only as an Authorization header, so the door reads Authorization and accepts exactly Bearer followed by the configured token, as the scraper named prometheus holding the scraper role. It is registered before "main", since matching is first-registered-wins and "main" matches every path; a missing or wrong credential authenticates as nobody and the json entry point answers 401. An empty token leaves the door unwired and /metrics public. */
func (instance *Module) registerMetricsFirewall(builder *melodysecurityconfig.Builder) {
    if "" == instance.metricsToken {
        return
    }

    builder.AddStatelessFirewall(
        "metrics",
        melodysecurity.NewPathPrefixMatcher(metricsPath),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewAuthenticatorTokenSource(
            melodysecurity.NewAuthenticatorManager(
                melodysecurity.NewApiKeyHeaderAuthenticator(
                    "Authorization",
                    "Bearer "+instance.metricsToken,
                    "prometheus",
                    []string{metricsScraperRole},
                ),
            ),
        ),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )
}

/* internalNonceGuardPrefix names the nonces this application's internal firewall remembers, so two applications on one redis never refuse each other's envelopes as replays */
const internalNonceGuardPrefix = "melody-example-v3:nonce"

/* internalNonceGuard is the replay guard of the internal firewall: on redis every replica remembers the nonces every other one accepted, so an envelope accepted by one is refused by all; without redis the source keeps its in-process guard, which a single process is. The envelope horizon bounds how long a nonce is remembered. */
func (instance *Module) internalNonceGuard() melodysecuritycontract.NonceGuard {
    if nil == instance.redisClient {
        return nil
    }

    return melodyrueidis.NewNonceGuardWithPrefix(instance.redisClient, internalNonceGuardPrefix)
}

