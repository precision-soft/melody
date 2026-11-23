# CONTRACT

This document lists every exported interface of the fourth major — the core module and each `integrations/*/v4` module — with who is expected to implement it, and what that promises from `v4.0.0` on. It is the one source of that answer: the [versioning policy](./UPGRADE.md#versioning-policy-for-breaking-changes) points here, and the api surface band reads it.

## The two classes

- **application** — an application, or a module outside this repository, is expected to implement it. It gains no method, loses none and changes none within v4; a new capability is a new small interface the framework probes by type assertion and documents.
- **framework** — the framework, or the integration that owns it, ships the only implementations meant to exist; an application calls it and may embed the shipped type, but an out-of-tree implementation is unsupported. It may gain a method in a MINOR, marked `**Breaking**` in the changelog for whoever implemented it anyway. It never loses or changes one within v4: its callers are owed that.

An interface an application implements only in its tests, such as a clock, an http client or a logger, is `application`: a broken test build is a broken build. A registrar a module hook is handed is the exception: the framework builds it, so it is `framework`, and a test spy that implements one does so at its own risk, following the method a MINOR adds.

"Within v4" starts at the tag `v4.0.0`. Until then a capability the framework prefers, or refuses to run without, is a method of its contract rather than an optional interface.

Moving an interface from `application` to `framework` is itself a break of the promise its implementers were given: it ships only with a `**Breaking**` entry in the changelog, which is the record of the move.

## How the list is checked

Every module below has one section, headed by its directory, and one row per exported interface: the name as `<package path relative to the module>.<Name>` (the bare name in the module's root package), linked to the file that declares it, the class, and the reason. An interface under `internal/`, an unexported one and a constraint that is a type set are not listed; an alias of an interface declared outside the module is listed under the alias.

[`.dev/validate/apidiff.sh`](../../.dev/validate/apidiff.sh) refuses a module whose interfaces and rows disagree, in both directions, a row whose class is neither of the two, a row with no reason, a second row for one interface and a link that is not the declaring file. From the first v4 tag it also refuses to ship a method added, removed or changed on an `application` interface, and a method removed or changed on a `framework` one; a method added to a `framework` interface ships in a MINOR, listed apart and marked `**Breaking**`.

## Module `v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`application.SecurityModule`](../application/security_module.go) | application | a module hook an application module implements to hand the security configuration to the boot |
| [`application/contract.BootWarningModule`](../application/contract/boot_warning_module.go) | application | a module hook: an application module implements it to add boot warnings |
| [`application/contract.BootWarningRegistrar`](../application/contract/boot_warning_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.CliModule`](../application/contract/cli_module.go) | application | a module hook: an application module implements it to register its commands |
| [`application/contract.ConfigModule`](../application/contract/config_module.go) | application | a module hook: an application module implements it to declare its configuration |
| [`application/contract.ConfigRegistrar`](../application/contract/config_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.EnvironmentKeyModule`](../application/contract/environment_key_module.go) | application | a module hook: an application module implements it to declare its environment keys |
| [`application/contract.EnvironmentKeyRegistrar`](../application/contract/environment_key_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.EventModule`](../application/contract/event_module.go) | application | a module hook: an application module implements it to register its listeners and subscribers |
| [`application/contract.HttpHandlerDecoratorModule`](../application/contract/http_handler_decorator_module.go) | application | a module hook: an application module implements it to decorate the route handlers |
| [`application/contract.HttpMiddlewareModule`](../application/contract/http_middleware_module.go) | application | a module hook: an application module implements it to add middleware |
| [`application/contract.HttpMiddlewareRegistrar`](../application/contract/http_middleware_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.HttpModule`](../application/contract/http_module.go) | application | a module hook: an application module implements it to register its routes |
| [`application/contract.Module`](../application/contract/module.go) | application | every application module and every integration module implements it |
| [`application/contract.ModuleProvider`](../application/contract/module.go) | application | an application implements it to hand its modules to the boot |
| [`application/contract.ParameterModule`](../application/contract/parameter_module.go) | application | a module hook: an application module implements it to declare its parameters |
| [`application/contract.ParameterRegistrar`](../application/contract/parameter_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.ProcessContext`](../application/contract/process_context.go) | framework | the boot builds the process context it hands to the modules |
| [`application/contract.ScopedServiceModule`](../application/contract/scoped_service_module.go) | application | a module hook: an application module implements it to register scoped services |
| [`application/contract.ScopedServiceRegistrar`](../application/contract/scoped_service_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`application/contract.ServiceModule`](../application/contract/service_module.go) | application | a module hook: an application module implements it to register its services |
| [`application/contract.ServiceRegistrar`](../application/contract/service_module.go) | framework | the boot hands it to a module; only the application builder implements it |
| [`bag/contract.ParameterBag`](../bag/contract/parameter_bag.go) | framework | the framework builds every parameter bag an application reads |
| [`cache/contract.Backend`](../cache/contract/backend.go) | application | a storage backend is written by the application or an integration (rueidis ships one) |
| [`cache/contract.Cache`](../cache/contract/cache.go) | application | an application may wrap or replace the cache it resolves, and its tests double it |
| [`cache/contract.Serializer`](../cache/contract/serializer.go) | application | the cache payload encoding is the application's to choose |
| [`cli/contract.Command`](../cli/contract/command.go) | application | every command of an application implements it |
| [`cli/contract.Flag`](../cli/contract/type.go) | application | urfave/cli v3's flag by alias: an application may write its own flag type, and the method set moves only with a urfave major, which is a melody major |
| [`cli/output.Printer`](../cli/output/printer.go) | framework | the framework ships the printers of the output formats it names; an application selects one |
| [`clock/contract.Clock`](../clock/contract/clock.go) | application | an application implements it in its tests to control time |
| [`clock/contract.Ticker`](../clock/contract/ticker.go) | application | returned by a clock an application implements in its tests |
| [`clock/contract.Timer`](../clock/contract/clock.go) | application | returned by a clock an application implements in its tests |
| [`config/contract.CliConfiguration`](../config/contract/cli.go) | framework | the framework builds the configuration it reads at boot |
| [`config/contract.Configuration`](../config/contract/configuration.go) | framework | the framework builds the configuration it reads at boot |
| [`config/contract.EnvironmentKeyDeclarer`](../config/contract/environment_key.go) | framework | the framework hands it to whoever declares an environment key |
| [`config/contract.EnvironmentKeyRule`](../config/contract/environment_key.go) | application | an application writes a rule to check its own environment key |
| [`config/contract.EnvironmentSource`](../config/contract/environment_source.go) | application | an application supplies the source its environment is read from |
| [`config/contract.HttpConfiguration`](../config/contract/http.go) | framework | the framework builds the configuration it reads at boot |
| [`config/contract.KernelConfiguration`](../config/contract/kernel.go) | framework | the framework builds the configuration it reads at boot |
| [`config/contract.Parameter`](../config/contract/parameter.go) | framework | the framework builds every parameter a configuration holds |
| [`container/contract.Container`](../container/contract/container.go) | framework | the kernel refuses a container without the framework's scope, close and boot guarantees |
| [`container/contract.ContextCloser`](../container/contract/context_closer.go) | application | a service an application registers implements it to be closed at teardown |
| [`container/contract.GoroutineResolver`](../container/contract/goroutine_resolver.go) | framework | the container builds the resolver it hands to another goroutine |
| [`container/contract.OverrideService`](../container/contract/override.go) | framework | the container's own override door |
| [`container/contract.Registrar`](../container/contract/registrar.go) | framework | the container's registration door |
| [`container/contract.Resolver`](../container/contract/resolver.go) | framework | the container's resolution door; an application calls it |
| [`container/contract.Runtime`](../container/contract/runtime.go) | framework | the kernel builds the runtime of every request, command and boot |
| [`container/contract.Scope`](../container/contract/scope.go) | framework | the container builds every scope |
| [`container/contract.ScopeManager`](../container/contract/scope.go) | framework | the container builds the scope manager |
| [`container/contract.ScopedRegistrar`](../container/contract/scoped_registrar.go) | framework | the container's scoped registration door |
| [`container/contract.TypeLister`](../container/contract/type_lister.go) | framework | the container lists its own types |
| [`event/contract.Event`](../event/contract/event.go) | framework | the dispatcher builds the event every listener receives |
| [`event/contract.EventDispatcher`](../event/contract/event_dispatcher.go) | framework | the kernel refuses a dispatcher without the framework's guarantees (required listeners, the floor) |
| [`event/contract.EventSubscriber`](../event/contract/event_subscriber.go) | application | an application subscriber implements it to declare its listeners |
| [`event/contract.PropagationStopper`](../event/contract/event.go) | application | an application payload implements it to stop the dispatch |
| [`event/contract.SubscribedEvent`](../event/contract/event_subscriber.go) | framework | the dispatcher builds the subscription it hands back |
| [`exception/contract.AlreadyLogged`](../exception/contract/already_logged.go) | framework | a marker the framework's logging sets on an error it already wrote |
| [`exception/contract.ContextProvider`](../exception/contract/context.go) | application | an application error implements it to add context to the log line |
| [`http.ServerSentEventBackplane`](../http/server_sent_event_hub.go) | application | a cross-process backplane is written by the application or an integration (amqp ships one) |
| [`http/contract.Kernel`](../http/contract/kernel.go) | framework | the framework's http kernel |
| [`http/contract.RateLimiter`](../http/contract/middleware.go) | application | a shared-store limiter is written by the application or an integration |
| [`http/contract.Request`](../http/contract/request.go) | framework | the kernel builds every request a handler receives; a test builds one through http.NewRequest |
| [`http/contract.RequestContext`](../http/contract/request_context.go) | framework | the kernel builds the request context |
| [`http/contract.Response`](../http/contract/response.go) | framework | the framework ships the response types a handler returns |
| [`http/contract.RouteDefinition`](../http/contract/route_definition.go) | framework | the router builds every route definition |
| [`http/contract.RouteGroup`](../http/contract/router_group.go) | framework | the router builds every route group |
| [`http/contract.RouteHandler`](../http/contract/router.go) | application | every handler of an application implements it |
| [`http/contract.RouteRegistry`](../http/contract/route_registry.go) | framework | the router's route registry |
| [`http/contract.Router`](../http/contract/router.go) | framework | the framework's router |
| [`http/contract.UrlGenerationRouteDefinition`](../http/contract/url_generation_route_definition.go) | framework | the router builds the definitions url generation reads |
| [`http/contract.UrlGenerator`](../http/contract/url_generator.go) | framework | the framework's url generator |
| [`httpclient/contract.Client`](../httpclient/contract/http_client.go) | application | an application implements it in its tests to answer outbound calls |
| [`httpclient/contract.Response`](../httpclient/contract/response.go) | framework | the framework's client builds every response |
| [`httpclient/contract.StreamResponse`](../httpclient/contract/stream_response.go) | framework | the framework's client builds every stream response |
| [`kernel/contract.Kernel`](../kernel/contract/kernel.go) | framework | the framework's kernel |
| [`lock/contract.Lock`](../lock/contract/lock.go) | framework | a locker returns it; the framework's lockers build every one |
| [`lock/contract.Locker`](../lock/contract/lock.go) | application | a shared-store locker is written by the application or an integration |
| [`logging/contract.Logger`](../logging/contract/logger.go) | application | an application implements it to route the log, and in its tests to read it |
| [`logging/contract.LoggingConfiguration`](../logging/contract/config.go) | framework | the framework builds the logging configuration |
| [`mailer/contract.Mailer`](../mailer/contract/mailer.go) | application | an application may replace the mailer it resolves, and its tests double it |
| [`mailer/contract.Transport`](../mailer/contract/mailer.go) | application | a delivery transport is written by the application or an integration |
| [`messagebus.ServiceRegistrar`](../messagebus/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |
| [`messagebus/contract.Bus`](../messagebus/contract/bus.go) | framework | the framework's bus |
| [`messagebus/contract.Envelope`](../messagebus/contract/envelope.go) | framework | the bus builds every envelope |
| [`messagebus/contract.HandlerLocator`](../messagebus/contract/locator.go) | application | an application may locate its own handlers |
| [`messagebus/contract.MessageHandler`](../messagebus/contract/handler.go) | application | every message handler of an application implements it |
| [`messagebus/contract.Stamp`](../messagebus/contract/envelope.go) | application | an application writes its own stamps |
| [`messagebus/contract.Subscription`](../messagebus/contract/transport.go) | application | a transport the application or an integration writes returns its own, so its author implements it with the `Transport` |
| [`messagebus/contract.Transport`](../messagebus/contract/transport.go) | application | a broker transport is written by the application or an integration (amqp ships one) |
| [`security.HmacApplicationRegistry`](../security/hmac_application_registry.go) | application | the application holds the registry of the services it trusts |
| [`security.HmacSecretProvider`](../security/hmac_secret_provider.go) | application | the application supplies the secrets the envelope is signed with |
| [`security/contract.AccessDecisionManager`](../security/contract/access_decision_manager.go) | framework | the framework's access decision manager |
| [`security/contract.AccessDeniedHandler`](../security/contract/access_denied_handler.go) | application | an application writes the answer a refused request receives |
| [`security/contract.Actor`](../security/contract/actor.go) | framework | the framework builds the actor a token carries |
| [`security/contract.ActorAware`](../security/contract/actor.go) | application | a token trait an application token implements |
| [`security/contract.ActorImpersonating`](../security/contract/actor.go) | application | a token trait an application token implements |
| [`security/contract.AuthenticationUnavailable`](../security/contract/token.go) | framework | a marker the framework's security errors carry |
| [`security/contract.Authenticator`](../security/contract/authenticator.go) | application | every authenticator of an application implements it |
| [`security/contract.EntryPoint`](../security/contract/entry_point.go) | application | an application writes the answer an unauthenticated request receives |
| [`security/contract.EpochRevocableTokenStore`](../security/contract/token_store.go) | application | a token store is written by the application or an integration |
| [`security/contract.FailedAttemptStore`](../security/contract/failed_attempt_store.go) | application | a shared failed-attempt store is written by the application or an integration |
| [`security/contract.Firewall`](../security/contract/firewall.go) | framework | the framework builds every firewall from its configuration |
| [`security/contract.FirewallManager`](../security/contract/firewall_manager.go) | framework | the framework's firewall manager |
| [`security/contract.ImpersonatedUserResolver`](../security/contract/impersonation.go) | application | the application resolves the user it impersonates |
| [`security/contract.Impersonating`](../security/contract/impersonation.go) | application | a token trait an application token implements |
| [`security/contract.JwtKeySet`](../security/contract/jwt_key_set.go) | application | the application supplies the keys a token is verified with |
| [`security/contract.LoginHandler`](../security/contract/login_handler.go) | application | an application writes its login answer |
| [`security/contract.LogoutHandler`](../security/contract/logout_handler.go) | application | an application writes its logout answer |
| [`security/contract.Matcher`](../security/contract/matcher.go) | application | an application writes its own request matcher |
| [`security/contract.NonceGuard`](../security/contract/nonce_guard.go) | application | a shared nonce store is written by the application or an integration |
| [`security/contract.RevocableTokenStore`](../security/contract/token_store.go) | application | a token store is written by the application or an integration |
| [`security/contract.RevocationEpochStore`](../security/contract/token_store.go) | application | an epoch store is written by the application or an integration |
| [`security/contract.RoleHierarchy`](../security/contract/role_hierarchy.go) | framework | the framework builds the role hierarchy from its configuration |
| [`security/contract.RoleHierarchyBinder`](../security/contract/role_hierarchy.go) | framework | the framework binds the role hierarchy |
| [`security/contract.Rule`](../security/contract/rule.go) | application | an application writes its own access rule |
| [`security/contract.Token`](../security/contract/token.go) | framework | the framework's authenticators and token sources build every token; an application reads it |
| [`security/contract.TokenEnricher`](../security/contract/token_enricher.go) | application | an application enriches the token it validated |
| [`security/contract.TokenSource`](../security/contract/token_source.go) | application | an application reads its token from its own source |
| [`security/contract.TokenStore`](../security/contract/token_store.go) | application | a token store is written by the application or an integration |
| [`security/contract.TokenValidator`](../security/contract/token_validator.go) | application | an application validates its own token format |
| [`security/contract.TwoFactorEnrollmentStore`](../security/contract/two_factor.go) | application | the application stores its two-factor enrollments |
| [`security/contract.TwoFactorPending`](../security/contract/two_factor.go) | application | a token trait an application token implements |
| [`security/contract.TwoFactorRecoveryStore`](../security/contract/two_factor.go) | application | the application stores its two-factor recovery codes |
| [`security/contract.TwoFactorRejection`](../security/contract/two_factor.go) | framework | a marker the framework's two-factor refusal carries |
| [`security/contract.Voter`](../security/contract/voter.go) | application | every voter of an application implements it |
| [`serializer/contract.Serializer`](../serializer/contract/serializer.go) | application | an application adds its own formats |
| [`session/contract.Manager`](../session/contract/manager.go) | framework | the framework's session manager |
| [`session/contract.Session`](../session/contract/session.go) | framework | the framework's manager builds every session |
| [`session/contract.SessionUnavailable`](../session/contract/session.go) | framework | a marker the framework's session errors carry |
| [`session/contract.Storage`](../session/contract/storage.go) | application | a session store is written by the application or an integration |
| [`storage/contract.Storage`](../storage/contract/storage.go) | application | an object store is written by the application or an integration (aws/s3 ships one) |
| [`translation/contract.Catalog`](../translation/contract/catalog.go) | application | an application supplies its own catalogues |
| [`translation/contract.Loader`](../translation/contract/catalog.go) | application | an application loads its catalogues from its own source |
| [`translation/contract.Translator`](../translation/contract/translator.go) | framework | the framework's translator |
| [`validation/contract.Constraint`](../validation/contract/constraint.go) | application | every constraint an application writes implements it |
| [`validation/contract.ParameterizedConstraint`](../validation/contract/constraint.go) | application | a constraint an application writes with parameters implements it |
| [`validation/contract.ValidationError`](../validation/contract/validation_error.go) | framework | the validator builds every validation error |

## Module `integrations/amqp/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`ServiceRegistrar`](../../integrations/amqp/v4/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |

## Module `integrations/aws/s3/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`ServiceRegistrar`](../../integrations/aws/s3/v4/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |

## Module `integrations/bunorm/migrate/v4`

The module exports no interface.

## Module `integrations/bunorm/mysql/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`ServiceRegistrar`](../../integrations/bunorm/mysql/v4/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |

## Module `integrations/bunorm/pgsql/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`ServiceRegistrar`](../../integrations/bunorm/pgsql/v4/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |

## Module `integrations/bunorm/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`Provider`](../../integrations/bunorm/v4/provider.go) | application | an application supplies the connection the module reads |
| [`audit.Storage`](../../integrations/bunorm/v4/audit/storage.go) | application | the application stores its audit trail where it chooses |
| [`audit.TransactionRider`](../../integrations/bunorm/v4/audit/storage.go) | application | the application rides its audit writes on its own transaction |
| [`encrypt.Cipher`](../../integrations/bunorm/v4/encrypt/cipher.go) | application | an application may bring its own cipher |
| [`encrypt.CipherReference`](../../integrations/bunorm/v4/encrypt/cipher_registry.go) | application | an application column names its cipher through it |
| [`encrypt.EncryptedColumn`](../../integrations/bunorm/v4/encrypt/encrypted_string.go) | framework | sealed by an unexported method, so only the column types the integration ships implement it |
| [`encrypt.KeyProvider`](../../integrations/bunorm/v4/encrypt/key_provider.go) | application | the application supplies the keys |
| [`encrypt.SealBindingReference`](../../integrations/bunorm/v4/encrypt/cipher_registry.go) | application | an application column names its seal binding through it |

## Module `integrations/cron/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`Template`](../../integrations/cron/v4/template.go) | application | an application writes the template its crontab is rendered with |

## Module `integrations/opentelemetry/v4`

The module exports no interface.

## Module `integrations/outbox/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`MessageCodec`](../../integrations/outbox/v4/codec.go) | application | an application encodes its own messages |
| [`Repository`](../../integrations/outbox/v4/repository.go) | application | the application stores its outbox; its doors are methods of the interface by verdict 11-18 |

## Module `integrations/rueidis/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`ServiceRegistrar`](../../integrations/rueidis/v4/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |
| [`cache.ServiceRegistrar`](../../integrations/rueidis/v4/cache/service_resolver.go) | framework | the boot's registrar satisfies it; only the application builder implements it |

## Module `integrations/websocket/v4`

| Interface | Implemented by | Reason |
|---|---|---|
| [`StreamConnection`](../../integrations/websocket/v4/handler.go) | framework | the module builds every stream connection a handler receives |
