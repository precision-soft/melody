# UPGRADE

This document records every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. It keeps one section per major release, newest first; the minor and patch releases since the last one are gathered under `Unreleased` until the next major promotes them.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

**Before the tag.** Every compatibility break of the fourth major lands before its freeze, on 2027-01-15. This document lists each one under `Unreleased`, with the symptom an upgrader sees and the remedy.

**After `v4.0.0`.** A PATCH repairs a defect, and nothing else ships until the first MINOR. A MINOR adds: a struct field, which is why configuration travels in structs, a function, an optional capability. No exported signature changes, no symbol leaves, and no default that protects is lowered.

**Interfaces.** Every exported interface of the major is listed in [`CONTRACT.md`](./CONTRACT.md) with who implements it. An `application` interface, one an application or a module outside this repository is expected to implement, gains no method, loses none and changes none within v4: a new capability is a new small interface the framework probes by type assertion and documents. A `framework` interface, one only the framework ships implementations of, may gain a method in a MINOR, marked `**Breaking**` for whoever implemented it anyway, and never loses or changes one. "Within v4" starts at the tag `v4.0.0`: until then a capability the framework prefers, or refuses to run without, is a method of its contract, which is why the slices before the tag folded their optional interfaces into the contracts they probed.

**Behaviour.** After the tag a behavioural change is a defect repair, or it waits for v5. A hardening that would refuse what a released v4 accepted ships as a warning in v4 and as a refusal in v5. Every protection on by default has a documented door that turns it off.

**What a v4 signature looks like**, so the rule can be checked by a reader: a required value is an argument, configuration is a struct, a bool's zero value is the safe side, no constructor takes variadic options, and a limit is a [`bound`](../bound/limit.go) value. A few shapes are kept on purpose although they read as exceptions:

- A struct one call takes is named `…Options`, a struct a constructor takes `…Config`: `RouteOptions`, `RegisterOptions`, `OverrideOptions`, `RequestOptions`, `PutOptions` and `SubscribeOptions` are handed to a call each time it runs, `ServerSentEventHubConfig` and `HandleMiddlewareConfig` once to the constructor that keeps them. A per-call door carries its struct before the tag, even an empty one or one of a single field, because a parameter cannot be added to a method afterwards and a field can.
- The close door keeps the name `CloseWithContext`: a type with `Close(ctx)` cannot also satisfy `io.Closer`, the suffix says at the call site that the close is bounded, and it is greppable. It is the major's one close name by decision, not a leftover `…Context` double.
- The flag and context types of [`cli/contract`](../cli/contract/type.go) are urfave/cli v3's by alias, so that library's method set is part of melody v4's surface: a urfave major is a melody major. Melody owns the command contract, `clicontract.Command`, and the output envelope, not the parser.
- The internal-auth envelope's wire key stays `app` although the Go names say `Application`: a fleet upgrades service by service, and a v3 signer must verify on a v4 verifier and back. The wire format is frozen across majors; the Go spelling is not.
- The migration tables default to bun's own `bun_migrations` and `bun_migration_locks`, not `melody_*`: a renamed default would make `db:migrate` on an existing database see no history and replay every migration. The two `.env` keys of the migrate module are the door for an application that wants the prefix.
- `MELODY_ENV` and its values `dev` and `prod` stay abbreviated: they are operator-facing strings in every deployed `.env`, and a rename would be a silent misconfiguration, an unknown value, where a renamed Go name fails to compile.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Unreleased

This section covers every change of the fourth major before its first tag, which this major's [`CHANGELOG.md`](../CHANGELOG.md) records under `[Unreleased]`. Each entry the changelog marks **Behavioural change** or **Breaking** has its section here, and the sections that answer no marked entry are notes beside them — an opt-in API, a contract repair the changelog already describes, an addition worth knowing before an upgrade — which ask nothing of an application that does not use what they name. The integration sections apply to an application that upgrades that integration module too; each module's own `CHANGELOG.md` records its entries and its release.

The fourth major starts from the third: the sections the third major shipped, in the `[v3.14.0]` block of its [`CHANGELOG.md`](../../v3/CHANGELOG.md) and the patch releases after it, are kept here because an application that upgrades from an earlier third-major release meets them too. Each of those is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. Two of them lost data — both in the `awss3` object storage integration, where a wrongly declared size could replace a stored object with a truncated one and then delete what was left.

The fourth major takes the forms the third major kept compatible: a section that names a sibling interface, a deprecated door or a released signature on the third major describes here the form the fourth major carries instead, and this major's changelog marks each **Breaking**.

### Rueidis: the client is opened under the caller's runtime and answered as its owner, and the module registers the owner's factory

**What changed.** [`Provider.Open`](../../integrations/rueidis/v4/provider.go) takes the caller's runtime first and answers the [`*Connection`](../../integrations/rueidis/v4/connection.go) that owns the client: the runtime's context refuses a done open before any client is built, ends the retry wait, which slept through a cancelled boot, and bounds the boot ping with `ClientConfig.BootPingTimeout`, 3s when left out, the earlier deadline winning. `Provider.Ping(runtimeInstance, connection)` runs under the runtime's context, with no deadline of its own. `Provider.Close`, which closed nothing an owner did not, `TimeoutConfig` and `ProviderConfig.Timeout` are removed. [`ModuleConfig`](../../integrations/rueidis/v4/module.go) registers a connection factory, `Connection`, built at boot under `ConnectionBuiltAtBoot` with the registrar's `ConnectionBootFailure`, and each role behind a pointer to its configuration, `Locker *LockerConfig` and `TokenStore *TokenStoreConfig`, nil registering none. The `Register…Service` functions of the module and of its cache package, `ServiceClient` and the cache module's `Client` are gone: the modules are the registration door, and every service they register resolves the connection service.

**Symptom.** `not enough arguments in call to provider.Open`; `cannot use … (variable of type *rueidis.Connection) as rueidis.Client value`; `provider.Close undefined`; `unknown field Timeout in struct literal of type rueidis.ProviderConfig`; `unknown field Client in struct literal of type rueidis.ModuleConfig`, `unknown field AsLocker` and `unknown field AsTokenStore`; `undefined: rueidis.RegisterClientService`, `rueidis.RegisterLockerService`, `rueidis.RegisterTokenStoreService`, `rueidis.ServiceClient` and `cache.RegisterBackendService`; `unknown field Client in struct literal of type cache.ModuleConfig`. An application that opened redis while it was wiring, before `Boot`, has no runtime to hand.

**Remedy.** Open in the connection's provider and hand the module its factory: `Connection: func(resolver containercontract.Resolver) (*rueidis.Connection, error) { return provider.Open(resolver.Runtime(), parameters) }`, with `ConnectionBuiltAtBoot: true` and `ConnectionBootFailure: container.BootFailureFailsBoot` for a boot that refuses an unreachable store. Use `connection.Client()` where a client is asked, and `connection.CloseWithContext(closeContext)` for `provider.Close`; registered through the module, the container closes it. `Client: rueidis.ClientConfig{BootPingTimeout: …}` for `TimeoutConfig.ConnectTimeout`; `CommandTimeout` has no successor, since `Ping` runs under the context handed. `Locker: &rueidis.LockerConfig{}` for `AsLocker: true` and `TokenStore: &rueidis.TokenStoreConfig{…}` for `AsTokenStore: true`. `rueidiscache.NewModule(rueidiscache.ModuleConfig{Backend: …})` beside the rueidis module, with no client. Build what needs the client, a limiter, a nonce guard, a backplane, in a provider that resolves `rueidis.ServiceConnection`, and reach it from what was built while wiring through each call's runtime.

### Amqp: a connection closes under `CloseTimeout`

**What changed.** The close of a connection a [`Transport`](../../integrations/amqp/v4/transport.go) or a [`ServerSentEventBackplane`](../../integrations/amqp/v4/server_sent_event_backplane.go) dialed itself, and of a dial that lands after their close began or that their close stopped waiting for, is bounded by the component's `CloseTimeout`, where it was bounded by its `CallTimeout`; the wait for the publishes in flight and every send keep `CallTimeout`.

**Symptom.** A transport or a backplane that set a short `CallTimeout` to bound its teardown now waits up to `CloseTimeout`, 30s by default or the provider's, for a broker that does not answer the close of its owned connection; one that set a long `CallTimeout` no longer waits that long.

**Remedy.** Set `CloseTimeout` on the transport, the backplane or the provider to the budget a connection's close may take.

### Amqp: a connection is opened under the caller's runtime and answered as its owner, and a component is handed its source

**What changed.** [`Provider.Open`](../../integrations/amqp/v4/connection.go) takes the caller's runtime first and answers the [`*amqp.Connection`](../../integrations/amqp/v4/connection_owner.go) that owns the client's connection and carries the provider's `CloseTimeout`; its `CloseWithContext` is the one close, idempotent, and `Raw()` reaches the client's `*amqp091.Connection`. The dial runs under the runtime's context, bounded by the new `ProviderConfig.DialTimeout` (30s, at least 1ms, unlimited refused) in place of the dsn's `connection_timeout`; `ProviderConfig.TlsConfig` and `ProviderConfig.ConnectionName` are new. `Provider.Close` and `Provider.CloseWithContext` are removed. An [`amqp.Dialer`](../../integrations/amqp/v4/connection_owner.go) is `func(dialContext context.Context) (*amqp.Connection, error)`, cancelled by the component when its close stops waiting. `NewTransport(queue, registry, source, config)` and `NewServerSentEventBackplane(hub, source, config)` take a required `ConnectionSource`, `amqp.FromConnection(connection)` or `amqp.FromDialer(dialer)`, and `Connection` and `Dialer` leave `TransportConfig` and `ServerSentEventBackplaneConfig`. [`ModuleConfig`](../../integrations/amqp/v4/module.go) takes factories, `Connection` for the connection service and `Transports` for each transport service; `ModuleConfig.CloseTimeout`, `ConnectionServiceConfig`, `RegisterConnectionService`, `RegisterTransportService` and `ServiceRegistrar` are removed, the module being the registration door.

**Symptom.** `not enough arguments in call to provider.Open`; `cannot use … (variable of type *amqp.Connection) as *amqp091.Connection value`; `unknown field Dialer in struct literal of type amqp.TransportConfig`, and `Connection`, and the backplane config's two; `cannot use func() (*amqp091.Connection, error) as amqp.Dialer value`; `provider.Close undefined`, `provider.CloseWithContext undefined`; `unknown field CloseTimeout in struct literal of type amqp.ModuleConfig`, `cannot use … as func(resolver containercontract.Resolver) (*amqp.Connection, error) value`; `undefined: amqp.ConnectionServiceConfig`, `amqp.RegisterConnectionService`, `amqp.RegisterTransportService`. A transport handed a connection and a dialer no longer exists. A dial against a host that accepts and never speaks fails after `DialTimeout` instead of the client's own bound, and `Provider.Dialer` panics on an empty dsn where the dial refused it.

**Remedy.** Hand the runtime, `provider.Open(runtimeInstance, dsn)`, and use `connection.Raw()` where the client's type is asked and `connection.CloseWithContext(closeContext)` for `provider.Close`. Hand the source as the third argument: `amqp.NewTransport(queue, registry, amqp.FromDialer(provider.Dialer(dsn)), config)` for a transport that must survive a loss, which then dials on first use, or `amqp.FromConnection(connection)` for one that borrows a connection. Write a dialer of your own as `func(dialContext context.Context) (*amqp.Connection, error)`, dialling under the context and wrapping a hand-dialled connection with `amqp.NewConnection(raw, amqp.ConnectionConfig{})`. Register through the module: `Connection: func(resolver containercontract.Resolver) (*amqp.Connection, error) { return provider.Open(resolver.Runtime(), dsn) }` and `Transports` of factories, a transport over a dialer being a factory that answers it; the close timeout is `ProviderConfig.CloseTimeout`, or `ConnectionConfig.CloseTimeout` for a hand-dialled connection. Set `DialTimeout` where the dsn carried `connection_timeout`.

### Application: one namespace names what the stores share

**What changed.** The application has one namespace, the framework key `MELODY_NAMESPACE`, whose default is [`ApplicationConfig.Namespace`](../application/application_new.go), whose default is `melody`, read as `Kernel().Namespace()` (see [CONFIG.md](package/CONFIG.md#application-namespace)). [`rueidis.NewProvider`](../../integrations/rueidis/v4/provider.go), [`rueidis.NewConnection`](../../integrations/rueidis/v4/connection.go) and [`amqp.NewProvider`](../../integrations/amqp/v4/connection.go) take it positionally, and the six rueidis components, `NewLocker`, `NewTokenStore`, `NewRateLimiter`, `NewNonceGuard`, `NewServerSentEventBackplane` and `cache.NewBackend` with `cache.NewBackendService`, take the `*rueidis.Connection` in place of the client. Every default name derives from the namespace: `<namespace>:rate_limit:`, `<namespace>:sse`, `<namespace>:nonce`, `<namespace>:token`, `<namespace>:cache:`, the amqp backplane's `<namespace>.sse` exchange, the exclusive command's `<namespace>:command:<name>` and the outbox relay's `<namespace>:outbox:relay`, each byte-identical under `melody`, a relay naming a `TransportName` taking `<namespace>:outbox:relay:<transport>`. A redis lock is keyed `<namespace>:lock:<name>`, where its bare name was the key: the one name that changes under the default namespace. Not namespaced, since a database or a broker topology is already the application's: a transport's queue, exchange and routing key, the migration tables, the outbox table, service, parameter and command names.

**Symptom.** `not enough arguments in call to rueidis.NewProvider`, `rueidis.NewConnection` and `amqp.NewProvider`; `cannot use client (variable of interface type rueidis.Client) as *rueidis.Connection value` at the six component constructors; `undefined: Namespace` in a fake of `configcontract.KernelConfiguration`. Behaviour: during a rolling deploy an old and a new replica holding a redis lock of one name do not exclude each other, one locking `x` and the other `<namespace>:lock:x`. After `MELODY_NAMESPACE` is set, every derived name changes at once: caches start cold, counters restart, tokens under the old prefix are not found and their sessions end, the server-sent event fan-out splits between old and new replicas for the length of the rollout, and the amqp backplane's old exchange stays on the broker.

**Remedy.** Hand the namespace, `configuration.Kernel().Namespace()`, `config.NamespaceFromRuntime(runtimeInstance)` where only a runtime is held, or `config.DefaultNamespace` to keep every name of the third major: `rueidis.NewProvider(namespace, config)`, `amqp.NewProvider(namespace, config)`, and `rueidis.NewConnection(client, namespace)` for a client built elsewhere. Hand the components the connection `Open` answered. Add `Namespace() string` to a kernel configuration fake. A deployment that stops the world before it starts the new release needs nothing for the lock; a rolling one sets `LockerConfig.KeyPrefixDisabled: true` on the new replicas until no replica runs the old release, then removes it in a second deploy that tolerates one overlap. Set `MELODY_NAMESPACE` at a release boundary; a component that must keep its old keys names them, `TokenStoreConfig{Prefix: "melody:token"}`, and the old amqp exchange is deleted by hand.

### Outbox: a store is configured, a row carries headers and a destination, and the schema is ensured under a runtime

**What changed.** [`outbox.NewStore(database, codec, config)`](../../integrations/outbox/v4/store.go) takes a `StoreConfig`: `TableName`, `outbox.DefaultTableName` (`melody_outbox`) when left out, a plain identifier, bound in every query, and `Headers`, a [`messagebus.HeaderLimitsConfig`](../messagebus/headers.go). `outbox.TableName` is removed; `store.TableName()` answers the table. `Enqueue(ctx, executor, message, options)` takes [`EnqueueOptions`](../../integrations/outbox/v4/repository.go)`{Headers, Transport}`, stored in two new nullable columns, `headers` and `transport`. `Repository.ClaimDueMessages` takes `ClaimOptions{Transport}` and claims the rows of that name alone, an empty name the rows that name none; `RelayConfig.TransportName` names the rows a relay drains. The relay publishes a row's headers as a `messagebus.HeadersStamp`. A relay naming a `TransportName` locks `<namespace>:outbox:relay:<transport>` by default, so the relays of two names drain side by side, the relay that names none keeping `<namespace>:outbox:relay`. `Pending.Table` names the table a row was claimed from, and a row of a table other than `melody_outbox` publishes under the message id `melody-outbox-<table>-<id>`, two tables numbering their rows alike; a row of the default table keeps `melody-outbox-<id>`. `EnsureSchema(runtimeInstance)` takes the runtime in place of a context and adds the two columns to an older table.

**Symptom.** `not enough arguments in call to outbox.NewStore` and `storeInstance.Enqueue`; `undefined: outbox.TableName`; a type implementing `outbox.Repository` no longer compiles, `ClaimDueMessages` having four parameters; `cannot use ctx (variable of interface type context.Context) as containercontract.Runtime value in argument to store.EnsureSchema`. On a table owned by the application's migrations, `could not claim due outbox messages` wrapping the database's unknown-column error for `transport`. Behaviour: a row enqueued with `Transport: "x"` and no relay configured `TransportName: "x"` is never published, and the relay that names none drains only the rows that name none.

**Remedy.** `outbox.NewStore(database, codec, outbox.StoreConfig{})`, `store.Enqueue(ctx, tx, message, outbox.EnqueueOptions{})`, `outbox.DefaultTableName` or `store.TableName()` for the constant; a `Repository` of your own takes `outbox.ClaimOptions` and filters on `Transport`; `store.EnsureSchema(resolver.Runtime())` in the provider that builds the store, or under any runtime the caller holds; `outbox.ModuleConfig.StoreBuiltAtBoot` builds that store inside `Boot`, so the schema is ensured under the boot runtime, its failure following `StoreBootFailure`, rather than under the first request that resolves it. Run `EnsureSchema`, or alter a migrated table before the new binary is deployed — MySQL: `ALTER TABLE melody_outbox ADD COLUMN headers TEXT NULL, ADD COLUMN transport VARCHAR(64) NULL;`, PostgreSQL: `ALTER TABLE melody_outbox ADD COLUMN IF NOT EXISTS headers TEXT NULL, ADD COLUMN IF NOT EXISTS transport VARCHAR(64) NULL;` — and, with two relays, index `(transport, status, available_at)`. Configure one relay per transport name. Stop every v3 relay on the table before a producer enqueues with `Transport` or `Headers`: a v3 relay claims the rows of every transport name, publishes them through its own transport and drops their headers.

### Messagebus, amqp: envelope headers cross the broker

**What changed.** [`messagebus.HeadersStamp`](../messagebus/stamp.go) is the one stamp every transport serializes beside the message id; [`messagebus.ValidateHeaders`](../messagebus/headers.go) refuses, before any write, a header name outside 1 to 64 characters of letters, digits, `.`, `_` and `-`, a value that is not valid utf-8 or holds a control character, and more headers or bytes than the writer's `HeaderLimitsConfig`, 32 and 8 KiB by default. The [amqp transport](../../integrations/amqp/v4/transport.go) writes each header as `x-melody-header-<name>` (`amqp.ApplicationHeaderPrefix`), bounded by `TransportConfig.Headers`, and rebuilds one `HeadersStamp` on receive; the in-memory transport judges `InMemoryTransportConfig.Headers` at `Send`.

**Symptom.** A consumer of another stack sees new `x-melody-header-*` headers; a producer of another stack that writes them is read as envelope headers, a prefixed header that is not a string or fails the header rule skipped with a warning naming it; a received envelope is requeued under the rule alone, never refused for more headers or bytes than this transport writes. A send that carried an application stamp expecting it to arrive: it still does not. A send whose headers break the rule is refused with an error caused by `messagebus.ErrHeadersRefused`, not marked as an outage.

**Remedy.** Put the strings a consumer needs in a `messagebus.HeadersStamp`; raise `TransportConfig.Headers`, `StoreConfig.Headers` or `InMemoryTransportConfig.Headers` where a message legitimately carries more.

### Cron: the in-process runner runs each tick of an entry on one replica

**What changed.** The [in-process cron runner](../../integrations/cron/v4/README.md#running-on-several-replicas) takes a per-tick lock for every due entry when the application registers `service.lock.locker` or names `RunnerConfig.TickDedup.Locker`, and keeps it for `TickDedup.Window` past the run, five minutes by default, plus `ClockJumpResetThreshold` for an entry that pins a minute and an hour. Each minute's document carries `skipped`. `lock.NewExclusiveCommand` is overlap protection: one run at a time, the lock released when the run returns; it never was once-per-tick.

**Symptom.** On several replicas a job that ran once per replica runs once, the others listing it under `skipped`. A pool sized tightly sees extra held connections when the registered locker is the mysql or pgsql one, one per held tick, five per every-minute entry at the default window. During a locker outage scheduled runs fail with `cron: the tick dedup locker could not be reached` instead of running, and `--once` exits non-zero. An application with no locker logs one warning at `Run`.

**Remedy.** Nothing for the intended behaviour. `EntryConfig{TickDedup: cron.EntryTickDedupConfig{Disabled: true}}` for a job that must run on every replica; `RunnerConfig{TickDedup: cron.TickDedupConfig{Disabled: true}}` for the third major's behaviour, which also silences the warning; `FailureMode: cron.TickDedupFailureRun` to run through an outage; a lease locker in `TickDedup.Locker`, or a shorter `Window`, where the registered one is a session locker. A command wrapped in `lock.NewExclusiveCommand` for dedup keeps the wrapper only if overlapping runs must be serialised, or when an external scheduler launches it.

### Http: the server-sent event hub has one close, takes its logger at construction and clears through `ClearBackplane`

**What changed.** [`ServerSentEventHub`](../http/server_sent_event_hub.go) closes through `CloseWithContext(closeContext)` alone: `Shutdown()` is removed, and a nil context is refused naming `door` `ServerSentEventHub.CloseWithContext`. `SetLogger` is removed: the hub's journal is `ServerSentEventHubConfig.Logger`, fixed at construction, nil recording nothing and a typed nil refused naming `field` `ServerSentEventHubConfig.Logger`. `ServerSentEventHubConfig.BackplaneClearTimeout` is removed with the bounded clear it configured: `SetBackplane(nil)`, and a typed nil, panics with `the server-sent event hub is handed a nil backplane`, `door` `ServerSentEventHub.ClearBackplane`, where it cleared the backplane under thirty seconds; `ClearBackplane(clearContext)` is the clear, under the caller's deadline, a nil context refused naming its door.

**Symptom.** `hub.Shutdown undefined`; `hub.SetLogger undefined`; `unknown field BackplaneClearTimeout in struct literal of type http.ServerSentEventHubConfig`. At run time, a `SetBackplane(nil)` that cleared the backplane panics.

**Remedy.** `hub.CloseWithContext(closeContext)` for `hub.Shutdown()`, `CloseWithContext(context.Background())` for the plain close; registered as a service, the container closes the hub. `NewServerSentEventHub(http.ServerSentEventHubConfig{Logger: logger})` for `SetLogger(logger)`: a hub built while wiring, before the application's logger exists, is built in a service provider that resolves the logger, and what was built while wiring reaches it through the request's or the run's runtime. Delete `BackplaneClearTimeout` and clear with `hub.ClearBackplane(clearContext)`, the bound being the context's deadline, `context.WithTimeout(context.Background(), 30*time.Second)` for the old default.

### Websocket: a stream's hub is resolved per connection

**What changed.** [`StreamConfig.Hub`](../../integrations/websocket/v4/module.go) is the hub's provider, `func(runtimeInstance containercontract.Runtime) (*http.ServerSentEventHub, error)`, where it was the hub: the module calls it on every connection with the connection's runtime, so a hub a container provider builds at boot, after the routes registered, is served by the module. A nil provider is refused at boot naming `ModuleConfig.Streams[<index>].Hub`; the provider's error is the request's, and a provider answering a nil hub refuses the request before the upgrade. The handler configuration is still resolved once, at the route registration, a limit outside its rule failing the boot. `NewStreamHandler(hub, config)` keeps the hub it is handed.

**Symptom.** `cannot use hub (variable of type *http.ServerSentEventHub) as func(runtimeInstance contract.Runtime) (*http.ServerSentEventHub, error) value in struct literal`.

**Remedy.** Hand the module a provider: `Hub: func(runtimeInstance containercontract.Runtime) (*http.ServerSentEventHub, error) { return runtime.FromRuntime[*http.ServerSentEventHub](runtimeInstance, serviceName) }` for a hub registered as a service, `func(containercontract.Runtime) (*http.ServerSentEventHub, error) { return hub, nil }` for one built while wiring. A route of the application's own that resolved the hub per request and called `NewStreamHandler` inside its handler is the module again.

### Http: `Broadcast` and a backplane's `Publish` take the caller's runtime

**What changed.** [`ServerSentEventHub.Broadcast`](../http/server_sent_event_hub.go) takes the runtime of the request, the command or the delivery it runs in, `Broadcast(runtimeInstance, topic, event) int`, a nil runtime panicking with `door` `http.ServerSentEventHub.Broadcast`, and [`ServerSentEventBackplane.Publish`](../http/server_sent_event_hub.go) takes it too, `Publish(runtimeInstance, topic, event) error`. The two shipped backplanes refuse a done context before they encode the event, `sse backplane publish refused: the caller's context is done` carrying the context's cause, and keep `CallTimeout` as the cap of one round trip within that context. A broadcast whose context is done still delivers to this instance's subscribers; its replication is refused and counted in `BackplaneFailures`.

**Symptom.** `not enough arguments in call to hub.Broadcast`; a type implementing `ServerSentEventBackplane` no longer satisfies it (`wrong type for method Publish`). At run time, a broadcast from a request cancelled mid-publish is no longer replicated to the other instances; its local delivery stands.

**Remedy.** Hand `Broadcast` the runtime in scope, the handler's, the command's or the message handler's. A backplane of your own takes the runtime and publishes under `runtimeInstance.Context()`. A broadcast that must outlive its request is handed `runtime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context()))`.

### Http, messagebus: a subscription and a dispatch take their options, and the handle middleware its config

**What changed.** [`ServerSentEventHub.Subscribe`](../http/server_sent_event_hub.go) takes a [`SubscribeOptions`](../http/server_sent_event_hub.go) beside the topic and answers an error, `Subscribe(topic, options) (*ServerSentEventSubscriber, error)`: `SubscribeOptions.Buffer` is the subscriber's own channel capacity, the hub's `SubscriberBuffer` when left out, at least 1 and unlimited refused, a refusal answered naming `SubscribeOptions.Buffer`; a closed hub still answers a closed, unregistered subscriber with no error. [`messagebuscontract.Bus.Dispatch`](../messagebus/contract/bus.go) takes a [`messagebuscontract.DispatchOptions`](../messagebus/contract/bus.go) in place of the stamps slice, `Dispatch(runtimeInstance, message, options)`, its `Stamps` added in order, nil for none. `messagebus.HandleOptions` is [`messagebus.HandleMiddlewareConfig`](../messagebus/middleware_handle.go), the same `AllowMissingHandler` field: it is what the constructor `NewHandleMessageMiddleware` keeps, so it takes the constructor's suffix.

**Symptom.** `not enough arguments in call to hub.Subscribe`, then `assignment mismatch: 1 variable but hub.Subscribe returns 2 values`; `cannot use nil as messagebuscontract.DispatchOptions value in argument to bus.Dispatch` or `cannot use stamps (variable of type []messagebuscontract.Stamp) as messagebuscontract.DispatchOptions value`; a type implementing `messagebuscontract.Bus` no longer satisfies it (`wrong type for method Dispatch`); `undefined: messagebus.HandleOptions`.

**Remedy.** `subscriber, subscribeErr := hub.Subscribe(topic, http.SubscribeOptions{})`, ending the stream on the error as on any other setup failure; `bus.Dispatch(runtimeInstance, message, messagebuscontract.DispatchOptions{})`, or `messagebuscontract.DispatchOptions{Stamps: stamps}`; a `Bus` of your own takes `options messagebuscontract.DispatchOptions` and reads `options.Stamps`; `messagebus.HandleMiddlewareConfig{}` where `messagebus.HandleOptions{}` stood. The suffix follows the rule the versioning policy above writes: `…Options` for what one call takes, `…Config` for what a constructor takes.

### Optional dependencies: nil is the default, a typed nil is refused; the bunorm registry takes a config, the sql modules a locker pointer

**What changed.** An optional logger or clock in a config struct reads one way in every constructor of the framework and its integrations ([optional dependencies](./package/CONTAINER.md#optional-dependencies)): nil is the default, the system clock or the emergency logger, never silence, and a typed nil is refused at construction as `<component> logger is a typed nil` or `<component> clock is a typed nil`, naming the field. [`bunorm.NewManagerRegistry`](../../integrations/bunorm/v4/manager_registry.go) takes the definitions and a `ManagerRegistryConfig{Logger}`, the logger optional, and `bunorm.ErrLoggerIsRequired` is gone. The [mysql](../../integrations/bunorm/mysql/v4/module.go) and [pgsql](../../integrations/bunorm/pgsql/v4/module.go) modules register their locker from `ModuleConfig.Locker *LockerConfig`, nil registering none; `AsLocker` and the refusal of a locker configuration with the locker off are gone, and so are their exported `RegisterLockerService` and `ServiceRegistrar`, and aws s3's `RegisterStorageService` and `ServiceRegistrar`: the module is the door. `logging.EnsureLogger` is replaced by [`logging.ResolveOptionalLogger`](../logging/optional_logger.go), beside [`clock.ResolveOptionalClock`](../clock/optional_clock.go).

**Symptom.** `too many arguments` or `cannot use logger as []bunorm.ProviderDefinition value` at `NewManagerRegistry`; `undefined: bunorm.ErrLoggerIsRequired`; `unknown field AsLocker in struct literal of type mysql.ModuleConfig` (and `pgsql`), `cannot use mysql.LockerConfig{} as *mysql.LockerConfig value`; `undefined: mysql.RegisterLockerService` (and `pgsql`), `undefined: awss3.RegisterStorageService`; `undefined: logging.EnsureLogger`. Behaviour: a server-sent event hub, an amqp or rueidis backplane, an audit recorder, a static file server or a session file storage built without a logger writes its failures to standard error through the emergency logger, where it recorded none; a typed-nil logger or clock that booted, on the rueidis provider, token store and backplane, the amqp backplane, the sql providers' `Open`, the in-memory cache, nonce guard, token store and session storage, the session manager, the rate limiters, the hmac and jwt sources, totp, now refuses the construction naming the field.

**Remedy.** `bunorm.NewManagerRegistry(definitions, bunorm.ManagerRegistryConfig{Logger: logger})`, and delete the `ErrLoggerIsRequired` branch. `Locker: &mysql.LockerConfig{}` where `AsLocker: true` was, and register the locker through `mysql.NewModule` (or `pgsql`), the object storage through `awss3.NewModule`. Hand a component the application's logger where its failures belong in the journal; pass nil, or the value, where a typed nil was handed. `logging.ResolveOptionalLogger(logger, component, field)` where `EnsureLogger` stood, or `logging.NewNopLogger()` where silence is wanted.

### Logging: a new journal is created `0640` under `0750` directories

**What changed.** [`logging.ReopenableFileWriterConfig`](../logging/reopenable_file_writer.go) carries `FileMode` and `DirectoryMode`, `logging.DefaultLogFileMode` (`0640`) and `logging.DefaultLogDirectoryMode` (`0750`) when left out, and the writer creates every missing directory of its path itself, on the first open and on every reopen. The container's journal and the logs directory the boot creates take `MELODY_LOG_FILE_MODE` and `MELODY_LOG_DIRECTORY_MODE`, octal, the same defaults, where both were `0644` and `0755`; the cache directory keeps `0755`. A mode is a creation mode only: a file or directory that already exists keeps its own. [`configcontract.KernelConfiguration`](../config/contract/kernel.go) declares `LogFileMode()` and `LogDirectoryMode()`.

**Symptom.** A log shipper or an operator outside the process's user and group can no longer read a journal created after the upgrade, or list its directory; an existing journal keeps its mode until a rotation creates the next one. The boot refuses a mode key that is empty, not octal, holds a bit outside `0777` or leaves out the owner's read and write (file) or read, write and search (directory), with `the journal file mode must be an octal permission that lets the owner read and write` or `the journal directory mode must be an octal permission that lets the owner read, write and search`, naming `environmentKey` and `value`; `NewReopenableFileWriter` answers the same refusal naming `ReopenableFileWriterConfig.FileMode` or `.DirectoryMode`. A `KernelConfiguration` of the application's own no longer compiles.

**Remedy.** Put the shipper in the process's group, or set `MELODY_LOG_FILE_MODE=0644` and `MELODY_LOG_DIRECTORY_MODE=0755` for the third major's modes; under logrotate's `create`, the mode written there is the one a rotated journal gets. Implement the two methods on an application's own `KernelConfiguration`.

### Security: an unsafe request that rides a session is checked for forgery

**What changed.** Every firewall checks, on `kernel.request` before access control, an unsafe request (anything but `GET`, `HEAD`, `OPTIONS` and `TRACE`) that presents the session cookie: a browser stating another origin, through `Sec-Fetch-Site` or `Origin`, is refused `403`; one stating its own origin passes in the default [`security.CsrfModeOrigin`](../security/csrf.go); one stating nothing over a session holding more than the csrf secret must present a token of [`http.CsrfToken`](../http/csrf.go), in the header `X-CSRF-Token` or the urlencoded field `_csrf_token`. A session the storage cannot answer at that step is refused `503` with the firewall's `Retry-After`. See [Cross-site request forgery](package/SECURITY.md#cross-site-request-forgery).

**Symptom.** A script, a test client or an old browser that holds the session cookie and states no origin gets `403 csrf token refused` on a write; a page on a sibling subdomain, or behind a proxy that rewrites `Host`, gets `403 cross-site request refused`.

**Remedy.** In order: send the token, from [`http.CsrfToken`](../http/csrf.go) rendered into the page or from the firewall's opt-in token route `FirewallConfig.Csrf.TokenPath`; list the other origin in `TrustedOrigins` of `FirewallConfig.Csrf` or `GlobalConfig.Csrf`; turn the check off for one route with `RouteOptions.Protection.CsrfDisabled`; set the firewall's `Csrf.Mode` or the global one to `security.CsrfModeDisabled`. No `.env` key turns it off: the switch is Go.

### Security: a firewall whose token source is stateless is not checked for forgery

**What changed.** A firewall whose [`TokenSource`](../security/contract/token_source.go) answers `Stateless()` true checks none of its routes for forgery; its login and logout doors still refuse a request stating another origin.

**Symptom.** None for the framework's bearer and HMAC sources. A bearer source of the application's own built through `NewResolverTokenSource` with the zero config is checked, and a write that also presents a session cookie and states nothing is asked the token.

**Remedy.** Declare such a source `ResolverTokenSourceConfig{Stateless: true}`, or `AuthenticatorTokenSourceConfig{Stateless: true}`.

### Security: the doors' `CrossSite` is `CsrfDisabled`

**What changed.** `LoginConfig.CrossSite`, `LogoutConfig.CrossSite`, `security.CrossSitePolicy`, `CrossSiteRefuse`, `CrossSiteAdmit` and `CompiledFirewallConfig.LoginCrossSite` / `LogoutCrossSite` are gone; a door's check is the firewall's forgery check, turned off by `LoginConfig.CsrfDisabled` / `LogoutConfig.CsrfDisabled` and on a hand-built firewall by `CompiledFirewallConfig.LoginCsrfDisabled` / `LogoutCsrfDisabled`.

**Symptom.** `unknown field CrossSite in struct literal of type LoginConfig`, `undefined: security.CrossSiteAdmit`.

**Remedy.** `CrossSite: security.CrossSiteAdmit` is `CsrfDisabled: true`; a trusted origin in `FirewallConfig.Csrf.TrustedOrigins` is the narrower door.

### Security: the sign-in drops the session's csrf secret

**What changed.** [`http.RegenerateRequestSession`](../http/session.go) deletes `http.SessionKeyCsrf` from the rotated session, so a token rendered before the sign-in does not verify after it.

**Symptom.** A page rendered before the sign-in and kept open posts a dead token and gets `403 csrf token refused`.

**Remedy.** Render the page, or read the token route, again after the sign-in.

### Http: every response carries security headers

**What changed.** Every response carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: strict-origin-when-cross-origin`, and a request resolved to `https` `Strict-Transport-Security: max-age=31536000`, through the `security_headers` middleware ahead of `static` and a `kernel.response` listener that reaches firewall refusals, error pages and not founds. `ApplicationConfig.SecurityHeaders` ([`middleware.SecurityHeadersConfig`](../http/middleware/security_headers.go)) sets each header's value, and `RouteOptions.Protection.SecurityHeadersDisabled` turns one off on one route. See [Security headers](package/HTTP.md#security-headers).

**Symptom.** A page framed by another page of the same application renders blank; a script or a stylesheet served under a wrong content type stops loading; behind TLS, or a trusted proxy stating `https`, browsers keep the host on `https` for a year; a test asserting a response's exact header set, or the `debug:middleware` listing, now meets `security_headers` first and `static` second.

**Remedy.** For the frame, `Protection.SecurityHeadersDisabled: []string{http.HeaderFrameOptions}` on the framed route or `FrameOptions: "SAMEORIGIN"`; serve the asset under its type; `StrictTransportSecurity: middleware.SecurityHeaderDisabled` while the host is not ready for `https`, or when the edge already sends the header; every header off: `SecurityHeaders: middleware.SecurityHeadersConfig{Disabled: true}`. No `.env` key turns them off: the switch is Go.

### Http: `Request.Post` and `Request.Input` answer an error

**What changed.** [`httpcontract.Request.Post()`](../http/contract/request.go) answers `(bagcontract.ParameterBag, error)` and [`Request.Input(key)`](../http/request.go) `(string, error)`: the error is the form read's, `413` with `http.ErrRequestBodyTooLarge` past the cap, `400 bad request` for a body that does not parse or a read that failed. See [Form parsing](package/HTTP.md#form-parsing).

**Symptom.** `assignment mismatch: 1 variable but request.Post returns 2 values`.

**Remedy.** Handle the error by returning it from the handler; the kernel answers the `413` or the `400`.

### Http: a urlencoded body is parsed by its first reader

**What changed.** `NewRequest` reads nothing of the body; the urlencoded form is parsed by the first `Post` or `Input`, and the kernel no longer answers a malformed or oversized form `400` or `413` before the handler.

**Symptom.** A `kernel.request` listener or a middleware reading `request.HttpRequest().PostForm` finds it nil; a route that never reads its form now serves a request whose urlencoded body is malformed or oversized; a request the firewall, the rate limiter or the access control refuses is refused without its body being read.

**Remedy.** Call `request.Post()` before reading `PostForm`, and return its error; a handler that must refuse a broken submission reads its form.

### Http: `Request.FormValue` and `Request.ParseFormBody` are gone

**What changed.** The two doors are removed: `FormValue` parsed a multipart body through net/http under its 32 MiB memory default, by accident, and `ParseFormBody` duplicated the form read. The framework parses no multipart body on any door.

**Symptom.** `request.FormValue undefined`, `request.ParseFormBody undefined`.

**Remedy.** `Input(key)` or `Post()` for a urlencoded form; for a multipart body, `request.HttpRequest().MultipartReader()` under the route's cap.

### Http: a route carries its own body cap

**What changed.** `RouteOptions.Protection.MaxBodyBytes`, a [`bound.Count`](../bound/limit.go), is the route's body cap, larger or smaller than `MELODY_HTTP_MAX_REQUEST_BODY_BYTES`, which every other route, an unmatched path and a firewall door keep; unlimited and a value below 1 are refused at registration. See [Request body limits](package/HTTP.md#request-body-limits).

**Symptom.** None.

**Remedy.** An application that raised `MELODY_HTTP_MAX_REQUEST_BODY_BYTES` for one upload route lowers the key back and sets `Protection.MaxBodyBytes` on that route.

### Validation: a declared request type with a wrong validate tag refuses the boot

**What changed.** A route that declares its body type, through `RouteOptions.RequestType`, [`http.HandleJson`](../http/typed_handler.go) or the openapi descriptor's `RequestType` of a named route, has the type read at boot, in every mode, by [`(*validation.Validator).VerifyType`](../validation/validator.go) against the validator the application registered: a tag that does not parse, an unknown rule, a parameter set its constraint refuses and a pattern that does not compile refuse the boot.

**Symptom.** The boot ends with `a route's request type declares a validate tag no value can pass`, naming each route and its type, every field, tag and rule under the cause.

**Remedy.** Fix the tag; register a custom constraint where the validator is built (its provider), not after boot; put `validate:"-"` on a field that must carry none. There is no switch: the route could never have served that body. A type bound without a declaration is still found by its first request, at the same status in every environment.

### Http: a route definition answers its request type

**What changed.** The framework-implemented [`httpcontract.RouteDefinition`](../http/contract/route_definition.go) gains `RequestType() reflect.Type`, the type the route's options declared.

**Symptom.** A type implementing `httpcontract.RouteDefinition`, a test fake, does not compile.

**Remedy.** Add `RequestType() reflect.Type { return nil }`.

### Http: `httpcontract.Request` gains `MaxBodyBytes`, and `Post` changes

**What changed.** The framework-implemented [`httpcontract.Request`](../http/contract/request.go) gains `MaxBodyBytes() int` and `Post` answers an error.

**Symptom.** A hand-written fake of the request no longer satisfies the interface.

**Remedy.** Build requests through `http.NewRequest`, with `RequestConfig.MaxBodyBytes` when no runtime carries the configuration.

### Security: the TOTP second factor locks an account after five refused factors in five minutes

**What changed.** `TotpSecondFactorAuthenticator` counts every presented factor, a TOTP code or a recovery code, per account before it verifies it; past `TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` (5) inside `LockoutWindow` (5m, a fixed window opened by the first factor) the factor is refused unread, a right one included, and the login door and `AuthenticatorTokenSource` answer `429 too many attempts` with the window's remainder as `Retry-After`, after `security.login.failure`. An accepted factor resets the count. See [Two-factor authentication](package/SECURITY.md#two-factor-authentication-totp).

**Symptom.** The sixth factor inside the window is answered `429`; a test presenting more than five wrong codes for one account turns red; on a firewall whose source is `AuthenticatorTokenSource`, every route of that request, a public one included, answers `429`.

**Remedy.** Tune `MaxFailedAttempts` and `LockoutWindow`; `MaxFailedAttempts: bound.UnlimitedCount()` keeps an application's own throttle as the only one; `(*TotpSecondFactorAuthenticator).ResetFailedAttempts(runtime, userIdentifier)` unlocks an account.

### Security: the second factor's count lives in the application's cache

**What changed.** `TotpSecondFactorAuthenticatorConfig.FailedAttemptStore` nil is a `security.CacheFailedAttemptStore` over `service.cache`, read from the runtime of each call, under `security.DefaultFailedAttemptKeyPrefix` (`melody:2fa:failed:`). A store that fails refuses the factor `503` with the firewall's `Retry-After`, where nothing was read before.

**Symptom.** An authenticator handed a runtime without `service.cache`, a hand-built test runtime, panics at the first presented factor naming the service; a cache `Clear` forgets every count; the in-memory backend may evict a counter past its `MaxItems` (10,000); a cache outage refuses the second factor `503`.

**Remedy.** Hand `FailedAttemptStore`, a `security.NewCacheFailedAttemptStore(security.CacheFailedAttemptStoreConfig{Cache: …})` over a cache of its own or an implementation of `securitycontract.FailedAttemptStore`.

### Security: `TwoFactorRejection` gains `SecondFactorLocked` and `SecondFactorRetryAfter`

**What changed.** The framework-implemented [`securitycontract.TwoFactorRejection`](../security/contract/two_factor.go) gains `SecondFactorLocked() bool` and `SecondFactorRetryAfter() time.Duration`; `security.NewTwoFactorLockedToken(pending, retryAfter)` builds the locked form.

**Symptom.** A type of the application's that implements `TwoFactorRejection` no longer satisfies it.

**Remedy.** Return the framework's token, `NewTwoFactorRejectedToken` or `NewTwoFactorLockedToken`, or embed `*security.TwoFactorPendingToken`.

### Security: the JWT validator takes a key set

**What changed.** [`NewJwtTokenValidator(keySet, config)`](../security/jwt_token_validator.go) takes a [`securitycontract.JwtKeySet`](../security/contract/jwt_key_set.go) as its first argument, a nil or typed-nil one refused naming the argument `keySet`; `JwtConfig.Secret` and `JwtConfig.MinimumSecretLength` are removed. [`NewJwtSecretKeySet(secret, JwtSecretKeySetConfig{})`](../security/jwt_secret_key_set.go) is the key set of one HS256 secret, its refusals naming the argument `secret` where they named `JwtConfig.Secret`, `MinimumSecretLength` moved to its config. The validator hands the set the token's `kid` and `alg` under the request's runtime and tries the answered keys in order, a key only under the algorithm it names; no key is `401`, a set that cannot answer `503`. `JwtConfig.MaxCandidateKeys` (8) bounds the keys one token is tried against, and a key whose secret is shorter than 32 bytes verifies nothing, answered `503`. A token with or without `kid` verifies against the secret key set as before.

**Symptom.** `security.JwtConfig{Secret: …}` and `MinimumSecretLength` no longer compile; `NewJwtTokenValidator` wants two arguments.

**Remedy.** `security.NewJwtTokenValidator(security.NewJwtSecretKeySet(secret, security.JwtSecretKeySetConfig{}), security.JwtConfig{…})`; a rotating or remote key source implements `securitycontract.JwtKeySet`. See [Key sets](package/SECURITY.md#key-sets).

### Application: the http server limits live under `ApplicationConfig.HttpServer`

**What changed.** [`ApplicationConfig.HttpServer`](../application/application_new.go), an `application.HttpServerConfig`, holds `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` and `ShutdownTimeout`, the flat `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` and `HttpShutdownTimeout` removed. Keys and defaults are unchanged. See [Http server](package/APPLICATION.md#http-server).

**Symptom.** `ApplicationConfig{ReadTimeout: …}` and its five siblings do not compile; a refusal that named `ApplicationConfig.ReadTimeout` names `ApplicationConfig.HttpServer.ReadTimeout`, and `ApplicationConfig.HttpShutdownTimeout` is `ApplicationConfig.HttpServer.ShutdownTimeout`.

**Remedy.** `ApplicationConfig{HttpServer: application.HttpServerConfig{ReadTimeout: …, ShutdownTimeout: …}}`; a log query or alert keyed on the old field names reads the nested ones.

### Http: the kernel is configured through `ApplicationConfig.HttpKernel`

**What changed.** [`ApplicationConfig.HttpKernel`](../application/application_new.go), an [`http.KernelConfig`](../http/kernel.go), is the http kernel's one source, built once at `NewApplication`. `(*http.Kernel).Use`, `SetNotFoundHandler`, `SetForwardedHeadersPolicy`, `SetSessionCookiePolicy`, `SetMethodPolicy`, `SetSessionPolicy` and `SetUnavailableRetryAfter` are removed from the kernel; `http.NewKernel(router, http.KernelConfig{…})` is the only way to configure a kernel. `ApplicationConfig.UnavailableRetryAfter` is `HttpKernel.UnavailableRetryAfter` and `ApplicationConfig.SessionEmptyOnOutage` is `HttpKernel.SessionPolicy.EmptyOnOutage`, their keys still winning; `HttpKernel.Middlewares` is refused at `NewApplication`, naming `RegisterHttpMiddlewares`. A module can no longer set a kernel policy: the composition root owns it. A policy a module set after the application's used to stand until the serve time put two of them back; now there is one value. See [Http kernel](package/APPLICATION.md#http-kernel).

**Symptom.** `kernelInstance.HttpKernel().SetNotFoundHandler(…)`, `SetForwardedHeadersPolicy`, `SetSessionCookiePolicy`, `SetMethodPolicy`, `SetSessionPolicy`, `SetUnavailableRetryAfter` and `Use` do not compile on `*http.Kernel`; `ApplicationConfig{UnavailableRetryAfter: …, SessionEmptyOnOutage: …}` does not compile; an `httpcontract.Kernel` field or conformance assertion does not compile either, the interface being removed (see [the served handler is asked of the application](#http-the-served-handler-is-asked-of-the-application)); `ApplicationConfig{HttpKernel: http.KernelConfig{Middlewares: …}}` exits at `NewApplication` with `the application's http middlewares are registered through RegisterHttpMiddlewares`.

**Remedy.** `ApplicationConfig{HttpKernel: http.KernelConfig{NotFoundHandler: …, ForwardedHeadersPolicy: …, SessionCookiePolicy: …, MethodPolicy: …, SessionPolicy: httpcontract.SessionPolicy{EmptyOnOutage: true}, UnavailableRetryAfter: bound.Of(…)}}`; a middleware through `Application.RegisterHttpMiddlewares` or a module's registrar; a bare kernel in a test through `http.NewKernel(router, http.KernelConfig{…})`. A value a module read from its own key and set on the kernel moves to the composition root; the session cookie's `Secure` has the framework key `MELODY_HTTP_SESSION_COOKIE_SECURE` (next section).

### Http: `MELODY_HTTP_SESSION_COOKIE_SECURE` marks the session cookie

**What changed.** The framework key `MELODY_HTTP_SESSION_COOKIE_SECURE` (`kernel.http.session_cookie_secure`), read through `Http().SessionCookieSecure()`, admits `always` alone and marks the cookie `Secure` on every response over `ApplicationConfig.HttpKernel.SessionCookiePolicy.Secure`, which is its default: the key only hardens; any other value, `from-scheme` and `never` included, refuses the boot naming the key, so only the Go field lowers the policy. `configcontract.HttpConfiguration` gains `SessionCookieSecure() string`. See [Session cookie secure](package/CONFIG.md#session-cookie-secure).

**Symptom.** An out-of-tree `configcontract.HttpConfiguration` does not compile; a deployment `.env` that set the key to `from-scheme` or another value refuses the boot.

**Remedy.** Add `SessionCookieSecure() string { return "" }`; an application key that switched the cookie through a module's `SetSessionCookiePolicy`, as the example's `APP_SESSION_COOKIE_SECURE` did, is replaced by the framework key; a deployment that wants the scheme's answer leaves the key empty and the field at `SessionCookieSecureFromScheme`.

### Http: the served handler is asked of the application

**What changed.** [`kernelcontract.Kernel`](../kernel/contract/kernel.go) no longer declares `HttpKernel()`, and the `httpcontract.Kernel` interface is removed. The application builds its `*http.Kernel` from `ApplicationConfig.HttpKernel` and keeps it; [`Application.HttpHandler()`](../application/application_http.go) answers the handler it serves, with the decorators, the serving marks and the exclusivity with `Run`, and `Application.BeginHttpShutdown(ctx)` starts its shutdown hooks for a server the application does not own. `kernel.NewKernel` takes six arguments again. See [Serving under another server](package/APPLICATION.md#serving-under-another-server).

**Symptom.** `kernelInstance.HttpKernel()` does not compile; nor does a reference to `httpcontract.Kernel`; a kernel of your own that declares `HttpKernel()` still compiles, the method unused.

**Remedy.** `application.HttpHandler()` for the handler, served under your own server, with `application.BeginHttpShutdown(ctx)` before its `Shutdown(ctx)` and `application.CloseWithContext(closeCtx)` after it; `http.NewKernel(router, http.KernelConfig{…}).ServeHttp(container)` for a bare kernel in a unit test; a field typed `httpcontract.Kernel` becomes `*http.Kernel`. Drop `HttpKernel()` from a kernel fake.

### Container, application, logging: one close door per owner

**What changed.** `containercontract.Scope`, [`*application.Application`](../application/application_close.go) and [`*logging.ReopenableFileWriter`](../logging/reopenable_file_writer.go) lose `Close()`; `CloseWithContext` is the one close door of each, and the owner's configured bound applies inside it, within the caller's context, the earlier ending the close: [`ContainerConfig.ScopeTeardownTimeout`](../container/container.go) (`MELODY_SCOPE_TEARDOWN_TIMEOUT`) for a scope, `TeardownTimeout` (`MELODY_TEARDOWN_TIMEOUT`) for an application that runs its own server; an application that handed its http handler out keeps the caller's context alone. A failure past the configured bound carries `bound` naming it. The json logger closes a writer through `CloseWithContext` first. [`audit.AsyncStorageConfig.CloseGrace`](../../integrations/bunorm/v4/audit/async_storage.go) is removed.

**Symptom.** `scope.Close undefined`, `app.Close undefined`, `writer.Close undefined`; a scope closed under a five-minute context with a 30s `ScopeTeardownTimeout` now ends at 30s; a `*ReopenableFileWriter` no longer fits an `io.Closer` parameter; `unknown field CloseGrace in struct literal of type audit.AsyncStorageConfig`.

**Remedy.** `CloseWithContext(context.Background())` for the old behaviour, or the caller's own context; raise the key, or declare it unlimited, for a longer close, since the argument can only shorten it; hand the writer to melody's logger, which closes it through its context door, or wrap it in a one-line adapter; delete `CloseGrace`, since `MELODY_TEARDOWN_TIMEOUT` bounds the drain.

### Security: a route's HMAC check order is a protection

**What changed.** The order an HMAC firewall checks a request's body and its nonce in is declared per route by `RouteOptions.Protection.HmacCheckOrder`, an [`httpcontract.HmacCheckOrder`](../http/contract/route_option.go): `HmacCheckOrderInherited`, the zero value and the source's own `VerifyBodyBeforeNonce`, `HmacCheckOrderNonceFirst` or `HmacCheckOrderBodyFirst`, winning over the source in either direction. `security.HmacVerifyBodyBeforeNonceAttribute` and `security.SetHmacVerifyBodyBeforeNonce` are removed. See [Internal service-to-service authentication](package/SECURITY.md#internal-service-to-service-authentication-hmac).

**Symptom.** `undefined: security.HmacVerifyBodyBeforeNonceAttribute` or `security.SetHmacVerifyBodyBeforeNonce`; a route that still writes `security.hmac.verifyBodyBeforeNonce` in `RouteOptions.Attributes` panics at registration with `route attribute is retired: declare RouteOptions.Protection.HmacCheckOrder`.

**Remedy.** `RouteOptions{Protection: http.RouteProtectionConfig{HmacCheckOrder: http.HmacCheckOrderBodyFirst}}` on the route. An order chosen per request, which the setter allowed, has no successor: declare two routes.

### Httpclient: the transport takes a TLS configuration and a proxy

**What changed.** [`TransportConfig`](../httpclient/transport_config.go) gains `TlsConfig`, `Proxy` and `ProxyDisabled`; their zero value is the client it was. See [Footguns & caveats](package/HTTPCLIENT.md#footguns--caveats).

**Symptom.** None. A comparison of two `TransportConfig` values with `==` no longer compiles: the struct holds a function.

**Remedy.** A `net/http` client built by hand only for a private certificate authority, a client certificate or a proxy comes back to `httpclient.NewHttpClient` with `TransportConfig.TlsConfig`, `Proxy` or `ProxyDisabled`, and keeps the client's timeouts, caps and credential stripping.

### Cli: the root is built from a config

**What changed.** [`cli.NewRoot(applicationName, applicationDescription, cli.RootConfig{Version, Writer, ErrorWriter})`](../cli/root.go) takes the version and the two streams at construction, stamped on every command at `Register`; `Root.SetVersion`, `SetWriter` and `SetErrorWriter` are removed. The zero config is an undeclared version and the process's standard streams.

**Symptom.** `root.SetVersion`, `SetWriter` and `SetErrorWriter` do not compile; `cli.NewRoot(name, description)` wants a third argument.

**Remedy.** `cli.NewRoot(name, description, cli.RootConfig{Version: …, Writer: …, ErrorWriter: …})`.

### Constructors: a value object takes a config

**What changed.** A constructor of a value object keeps the values that name it positional and takes the rest in a config struct whose zero value is the empty or default value, and the doors over a fixed value or a positional optional gain one:

- [`output.NewMeta`](../cli/output/envelope_factory.go): `NewMeta(command, arguments, option, startedAt, duration, version)` is `NewMeta(command, output.MetaConfig{Arguments, Option, StartedAt, Duration, Version})`; an empty Melody or Go version is still the running binary's and the application's is still the option's.
- [`output.NewError`](../cli/output/envelope_factory.go): `NewError(code, message, details, cause)` is `NewError(code, message, output.ErrorConfig{Details, Cause})`. `output.NewWarning(code, message, details)` is unchanged.
- [`output.NewListPayload`](../cli/output/list_payload.go): `NewListPayload(items, total, limit, offset)` is `NewListPayload(items, output.ListPayloadConfig{Total, Limit, Offset})`.
- [`httpclient.NewResponse`](../httpclient/response.go): `NewResponse(statusCode, status, headers, body, request)` is `NewResponse(statusCode, httpclient.ResponseConfig{Status, Headers, Body, Request})`.
- [`httpclient.NewStreamResponse`](../httpclient/stream_response.go): `NewStreamResponse(statusCode, headers, body)` is `NewStreamResponse(statusCode, body, httpclient.StreamResponseConfig{Headers})`.
- [`security.NewActor`](../security/actor.go): `NewActor(identifier, actorType, roles, attributes, impersonator)` is `NewActor(identifier, actorType, security.ActorConfig{Roles, Attributes, Impersonator})`; a typed-nil impersonator is refused naming `ActorConfig.Impersonator`.
- [`cache.NewItem`](../cache/item.go): `NewItem(key, payload, createdAt, expiresAt)` is `NewItem(key, payload, cache.ItemConfig{CreatedAt, ExpiresAt})`.
- [`security.NewBearerTokenSource`](../security/bearer_token_source.go): `NewBearerTokenSource(validator, enricher)` is `NewBearerTokenSource(validator, security.BearerTokenSourceConfig{Enricher})`; a typed-nil enricher is refused naming `BearerTokenSourceConfig.Enricher`.
- [`security.NewJsonEntryPoint`](../security/json_entry_point.go): `NewJsonEntryPoint()` is `NewJsonEntryPoint(security.JsonEntryPointConfig{Challenge})`; the 401's `WWW-Authenticate` is the challenge, `security.DefaultJsonEntryPointChallenge` (`Bearer`) when left out, and a challenge holding a control character or a line break is refused at construction naming `JsonEntryPointConfig.Challenge`.
- [`logging.NewReopenableFileWriter`](../logging/reopenable_file_writer.go): `NewReopenableFileWriter(path)` is `NewReopenableFileWriter(path, logging.ReopenableFileWriterConfig{})`; the struct carries the [creation modes](#logging-a-new-journal-is-created-0640-under-0750-directories).
- [`http.NewRequest`](../http/request.go): `NewRequest(httpRequest, routeParameters, runtimeInstance, requestContext)` is `NewRequest(httpRequest, runtimeInstance, http.RequestConfig{RouteParameters, RequestContext})`; a request built with neither still has no route parameter and answers a nil `RequestContext()`.
- [`lock.NewInMemoryLocker`](../lock/in_memory.go): `NewInMemoryLocker(clock)` is `NewInMemoryLocker(lock.InMemoryLockerConfig{Clock, PurgeInterval})`; a clock left out is the system clock, where a nil clock was refused, and a typed nil is refused naming `InMemoryLockerConfig.Clock`. `PurgeInterval` is the acquisitions between two sweeps of the expired holders, `lock.DefaultInMemoryLockerPurgeInterval` (512) when left out, at least 1, `bound.UnlimitedCount()` leaving the sweep to `PurgeExpired`.
- [`mailer.NewManager`](../mailer/manager.go): `NewManager(transport)` is `NewManager(transport, mailer.ManagerConfig{})`; the struct holds no field yet.
- [`output.NewTablePrinter`](../cli/output/table_printer.go): `NewTablePrinter(width)`, `NewDefaultTablePrinter()` and `NewUnboundedTablePrinter()` are one `NewTablePrinter(output.TablePrinterConfig{MaxWidth})`: 120 when left out, `bound.UnlimitedCount()` no cap, `bound.Of(width)` the width; a width below 1 is refused naming `TablePrinterConfig.MaxWidth`, where it fell back to 120.
- [`BootWarningRegistrar.DeclareBootWarning`](../application/contract/boot_warning_module.go): `DeclareBootWarning(name, writtenInCli)` is `DeclareBootWarning(name, applicationcontract.BootWarningConfig{WrittenInCli})`, and a registrar of the application's own, a test spy among them, takes the config.

**Symptom.** `too many arguments in call to output.NewMeta`, `output.NewError`, `output.NewListPayload`, `httpclient.NewResponse`, `httpclient.NewStreamResponse`, `security.NewActor`, `cache.NewItem` or `security.NewBearerTokenSource`; `not enough arguments in call to security.NewJsonEntryPoint`, `logging.NewReopenableFileWriter`, `lock.NewInMemoryLocker`, `mailer.NewManager` or `output.NewTablePrinter`; `too many arguments in call to http.NewRequest`; `undefined: output.NewDefaultTablePrinter` or `output.NewUnboundedTablePrinter`; `cannot use true (untyped bool constant) as applicationcontract.BootWarningConfig value` at `DeclareBootWarning`, and a registrar of the application's own that no longer implements `BootWarningRegistrar`; a width below 1 handed to the table printer panicking naming `TablePrinterConfig.MaxWidth`; a typed-nil impersonator or enricher refused with a context naming `field` where it named `argument`.

**Remedy.** Move each value after the positional ones into the config field of its name and leave out the nil, zero or empty ones: `output.NewMeta(name, output.MetaConfig{Arguments: arguments, Option: option, StartedAt: startedAt})`, `output.NewListPayload(items, output.ListPayloadConfig{Total: total, Limit: option.Limit, Offset: option.Offset})`, `security.NewActor(identifier, actorType, security.ActorConfig{Roles: roles})`, `security.NewBearerTokenSource(validator, security.BearerTokenSourceConfig{})`, `security.NewJsonEntryPoint(security.JsonEntryPointConfig{})`, `logging.NewReopenableFileWriter(path, logging.ReopenableFileWriterConfig{})`, `http.NewRequest(httpRequest, runtimeInstance, http.RequestConfig{RouteParameters: parameters})`, `lock.NewInMemoryLocker(lock.InMemoryLockerConfig{})`, `mailer.NewManager(transport, mailer.ManagerConfig{})`, `output.NewTablePrinter(output.TablePrinterConfig{})` for the default printer, `{MaxWidth: bound.UnlimitedCount()}` for the unbounded one and `{MaxWidth: bound.Of(width)}` for a width, and `registrar.DeclareBootWarning(name, applicationcontract.BootWarningConfig{WrittenInCli: true})`.

### Framework-only constructors left the surface

**What changed.** Seven constructors of values only the framework builds are unexported: `http.NewRouteDefinition`, `http.NewHttpRequestProfile`, `http.NewErrorResponsePayload`, `pipeline.NewMiddlewareBuildReport`, `config.NewParameter`, `security.NewMatchedAccessControlRule` and `event.NewRequiredListenerSkippedError`; [`kernel.NewKernel`](../kernel/kernel.go) takes as its first argument a token only the framework can build and refuses any other, so the application builds its kernel and userland does not. The types stay exported and readable. [`(*HttpMiddleware).LastBuildReport`](../application/http_middleware.go) answers an empty report before the first build, its kernel environment empty.

The doors the framework alone writes at boot follow: `MiddlewareBuildReport.SetSelectedNames`, `SetInactive`, `SetMissingReference` and `SetCycleDetected`, `pipeline.NewInactiveMiddleware`, `http.NewUrlGenerationRouteDefinition` and `SecurityContext.SetMatchedRule` are unexported. `RouteRegistry.SetBootCollisionRecorder` is [`RouteRegistry.InstallBootCollisionRecorder`](../http/route_registry.go), and [`Configuration.MarkServing`](../config/configuration.go) and [`openapi.Registry.MarkServing`](../openapi/registry.go) take a token only the framework can build, and each refuses any other with a panic naming the door, so an application can neither arm nor disarm the boot collision recorder nor end its own wiring phase early. [`application.NewHttpMiddleware`](../application/http_middleware.go) takes the static file server config alone, the configuration it never read dropped.

**Symptom.** `undefined: http.NewRouteDefinition`, `http.NewHttpRequestProfile`, `http.NewErrorResponsePayload`, `pipeline.NewMiddlewareBuildReport`, `config.NewParameter`, `security.NewMatchedAccessControlRule` or `event.NewRequiredListenerSkippedError`; `not enough arguments in call to kernel.NewKernel`; `report.SetInactive undefined`, `undefined: pipeline.NewInactiveMiddleware`, `http.NewUrlGenerationRouteDefinition`, `registry.SetBootCollisionRecorder undefined`; `not enough arguments in call to configuration.MarkServing` or `registry.MarkServing`; `too many arguments in call to application.NewHttpMiddleware`.

**Remedy.** Build the application through `application.NewApplication`, which builds the kernel. A parameter is registered through `Configuration.RegisterRuntime` or `RegisterRuntimeSecret` and read back through `Get`. A test of a `debug:middleware` provider builds its report through `pipeline.NewBuilder(definitions).Describe(environment, group)`, the inactive entries the definitions the environment or the group leave out. `Application.Run` arms the collision recorder and marks the configuration and the openapi registry serving; a registry built by hand and never marked keeps admitting descriptions. `NewHttpMiddleware(staticConfig, configuration)` is `NewHttpMiddleware(staticConfig)`. A `RequiredListenerSkippedError` is met on the dispatch that skips a required listener, asserted on the error the dispatch returns. A route definition, a request profile, an error payload and a matched rule are read from the router, the profiler, the response body and the access control listener that build them.

### Constructors: a required value is an argument

**What changed.** A value a constructor cannot do without leaves its config struct and becomes a positional argument before the config, so the compiler refuses the call that leaves it out; a nil or empty one is still refused with the same message, its context naming `argument` instead of `field`:

- [`security.NewHmacTokenSource`](../security/hmac_token_source.go): `NewHmacTokenSource(HmacTokenSourceConfig{Secrets: secrets, Applications: applications, …})` is `NewHmacTokenSource(secrets, applications, HmacTokenSourceConfig{…})`.
- [`security.NewTotpSecondFactorAuthenticator`](../security/totp_second_factor_authenticator.go): `Primary` and `Enrollments` are `NewTotpSecondFactorAuthenticator(primary, enrollments, TotpSecondFactorAuthenticatorConfig{…})`.
- [`security.NewImpersonationTokenSource`](../security/impersonation_token_source.go): `Inner` and `Users` are `NewImpersonationTokenSource(inner, users, ImpersonationTokenSourceConfig{…})`.
- [`security.NewApiKeyHeaderAuthenticator`](../security/api_key_authenticator.go) and [`security.NewApiKeyHeaderRule`](../security/rule.go): `ExpectedValue` is `NewApiKeyHeaderAuthenticator(expectedValue, config)` and `NewApiKeyHeaderRule(matcher, expectedValue, config)`; `HeaderName` stays in the struct and is `security.DefaultApiKeyHeaderName` (`X-Api-Key`) when empty instead of refused.
- [`mailer.NewSmtpTransport`](../mailer/smtp_transport.go): `SmtpConfig.Address` is `NewSmtpTransport(address, SmtpConfig{…})`.
- [`middleware.SimpleRateLimit`, `IpRateLimit` and `UserRateLimit`](../http/middleware/rate_limit.go): `QuickLimitConfig.RequestsPerMinute` is `SimpleRateLimit(requestsPerMinute, config)` and `IpRateLimit(requestsPerMinute, config)`; `UserLimitConfig.RequestsPerMinute` and `UserId` are `UserRateLimit(requestsPerMinute, userId, config)`.
- [`amqp.NewTransport`](../../integrations/amqp/v4/transport.go) and `Provider.NewTransport`: `Queue` and `Registry` are `NewTransport(queue, registry, source, TransportConfig{…})`, the connection source the third argument.
- [`amqp.NewServerSentEventBackplane`](../../integrations/amqp/v4/server_sent_event_backplane.go) and `Provider.NewServerSentEventBackplane`: `Hub` is `NewServerSentEventBackplane(hub, source, ServerSentEventBackplaneConfig{…})`.
- [`outbox.NewRelay`](../../integrations/outbox/v4/relay.go): `Repository`, `Transport` and `Codec` are `NewRelay(repository, transport, codec, RelayConfig{…})`.
- [`security.NewHmacEnvelopeSigner`](../security/hmac_signer.go): `HmacEnvelopeSignerConfig.Application` is `NewHmacEnvelopeSigner(runtimeInstance, secrets, application, HmacEnvelopeSignerConfig{…})`, an empty application now naming `argument: application`.
- [`awss3.NewClient`](../../integrations/aws/s3/v4/provider.go): `Config.Endpoint` is `NewClient(endpoint, Config{…})`; `Config` no longer prints an endpoint.
- [`otlp.NewTracerProvider`](../../integrations/opentelemetry/v4/otlp/tracer_provider.go): `Config.Endpoint` is `NewTracerProvider(runtimeInstance, endpoint, Config{…})`, and the module's is `otlp.ModuleConfig.Endpoint`, beside its `Config`.
- [`session.NewFileStorage`](../session/file_storage.go): `FileStorageConfig.Path` is `NewFileStorage(path, config)` and `FileStorageConfig.File` is the new `NewFileStorageOverFile(file, config)`; the refusals of both or neither are gone, an empty path and a nil file refused instead.

**Symptom.** `unknown field Secrets in struct literal of type security.HmacTokenSourceConfig` (and `Applications`, `Primary`, `Enrollments`, `Inner`, `Users`, `ExpectedValue`, `Address`, `RequestsPerMinute`, `UserId`, `Queue`, `Registry`, `Hub`, `Repository`, `Transport`, `Codec`, `Application`, `Endpoint`, `Path` and `File` likewise), and `not enough arguments in call to NewHmacTokenSource` for each constructor above. Code that reads the refusal's `field` key for these values finds it under `argument`.

**Remedy.** Move each value out of the literal into the argument before the config, in the order above, and build a storage over an open handle with `session.NewFileStorageOverFile(file, config)`. An api key header that was `X-Api-Key` may drop `HeaderName`.

### Hardening: the safe value of a switch is its zero value

**What changed.** Four switches that switched a protection on are inverted, so that leaving the field out keeps the protection and setting it is the documented door:

- [`SmtpConfig.RequireTls`](../mailer/smtp_transport.go) is `AllowPlaintext`: by default a relay that does not offer STARTTLS refuses the send, and `AllowPlaintext: true` sends to such a relay in clear, still upgrading when STARTTLS is offered. `AllowPlaintext` beside `ImplicitTls` is refused at construction, naming both fields.
- [`JwtConfig.RejectFutureIssuedAt`](../security/jwt_token_validator.go) is `AllowFutureIssuedAt`: by default a token whose `iat` is ahead of the clock by more than the leeway and one second is refused as `jwt is issued in the future`. `AllowFutureIssuedAt` beside a `RevocationEpochStore` is refused at construction, since the boundary is measured on `iat`; the third major forced the check under a store whatever the field said.
- [`amqp.TransportConfig.DeadLetter`](../../integrations/amqp/v4/transport.go) is `DeadLetterDisabled`: by default the transport declares `<queue>.dlx` and `<queue>.dlq` and points the main queue at them, so a poison message is kept rather than dropped.
- [`encrypt.SealBindingConfig.RefuseUnbound`](../../integrations/bunorm/v4/encrypt/module.go) and [`encrypt.ColumnSealBinding.RefuseUnbound`](../../integrations/bunorm/v4/encrypt/migrate.go) are `UnboundAdmitted`: by default a bound column reads nothing but a seal bound to it.

**Symptom.** `unknown field RequireTls in struct literal of type mailer.SmtpConfig` (and `RejectFutureIssuedAt`, `DeadLetter`, `RefuseUnbound` likewise). Then, once it compiles:

- every send to a development relay without STARTTLS (mailpit, a local MTA) fails with `smtp server does not offer STARTTLS but tls is required`, the context naming `SmtpConfig.AllowPlaintext`;
- a token from an issuer whose clock runs ahead is refused;
- a transport whose queue was declared before without the dead-letter argument fails to open with `amqp queue declare failed`, the broker answering `PRECONDITION_FAILED` (406, inequivalent arg `x-dead-letter-exchange`) and the context naming `TransportConfig.DeadLetterDisabled`;
- a bound column still holding `gcm1` seals or plain values refuses them with `ErrUnboundValueRefused`.

**Remedy.**
- Drop `RequireTls: true`, `RejectFutureIssuedAt: true`, `DeadLetter: true` and `RefuseUnbound: true`; they are the default now.
- Where the old zero value was meant:
  - a relay without STARTTLS takes `AllowPlaintext: true`;
  - an issuer whose clock cannot be fixed takes a wider `Leeway` or `AllowFutureIssuedAt: true`;
  - an existing queue is drained, deleted and declared again, or keeps its old arguments with `DeadLetterDisabled: true`, the `<queue>.dlx` exchange and `<queue>.dlq` queue the refused default declare left on the broker then deleted by hand;
  - a column being bound is deployed with `UnboundAdmitted: true` until `melody:encrypt:database --mode=encrypt` has sealed it under the binding, then the field comes off.

### Wiring: a constructor receives the resolution's context, and a process service keeps no request

**What changed.** The generator hands a `context.Context` argument the context of the resolution that builds the service and a `containercontract.Runtime` argument its runtime, where it generated a lookup of a service of that type that failed at the first resolution. It refuses, for a constructor not declared `//melody:scoped`, an argument of a request value (`httpcontract.Request`, `RequestContext`, `Response`, `sessioncontract.Session`, `securitycontract.Token`, `containercontract.Scope`) and a returned struct of the scanned packages that keeps a `context.Context`, a `context.CancelFunc` or a `containercontract.Runtime` in a field while the constructor takes the resolution's context or runtime. `//melody:keeps-context` is the door of the second refusal; a pointer to a context is refused.

**Symptom.** `melody:wiring:generate` fails with `a process service cannot be built from a value of one request` or `a process service that takes its resolution cannot keep a context`, naming the constructor, the argument or field, the file and the line.

**Remedy.** Declare the constructor `//melody:scoped` when the service belongs to one request; otherwise take the value or the context at each call. A worker that derives a context of its own at construction, detached from the request that resolved it first, carries `//melody:keeps-context`.

### Websocket: the stream handler's config and its message callback

**What changed.** [`websocket.Options`](../../integrations/websocket/v4/handler.go) is `StreamHandlerConfig`, `StreamConfig.Options` is `StreamConfig.Handler`, and the refusals name `StreamHandlerConfig.<field>`. `OnMessage` is `func(runtimeInstance containercontract.Runtime, connection StreamConnection, message InboundMessage) error`, melody types only: `InboundMessage{Binary, Payload}` carries the frame, a nil error keeps the connection, and a non-nil error is logged once at error (`websocket OnMessage failed`) and closes the connection with status 1011, as a panicking callback does where it closed with 1000. `StreamConnection.Send(runtimeInstance, OutboundMessage{Binary, Payload})` writes a frame to that connection from the callback or from any goroutine holding it, serialised with the broadcast writes, bounded by `WriteTimeout` and the runtime's context, refused with `ErrStreamConnectionClosed` once the connection closes, joined with the library's error when the close meets the write. The 1011 close follows a broadcast frame still being written, and a peer that never echoes it does not hold the handler past `CloseHandshakeGrace`. A runtime context that ends while a frame is being written makes the library close the whole connection. A callback that returns the error of a `Send` the client hung up on is journaled at error: filter `ErrStreamConnectionClosed` to keep client churn out of the error records.

**Symptom.** `undefined: websocket.Options`; `unknown field Options in struct literal of type websocket.StreamConfig`; `cannot use func(…, coderwebsocket.MessageType, []byte) (value of type func(…)) as func(…, websocket.StreamConnection, websocket.InboundMessage) error value`.

**Remedy.** Rename the struct and the field, read `message.Binary` and `message.Payload` where the callback took the message type and the payload, and return nil to keep the connection; reply with `connection.Send(runtimeInstance, websocket.OutboundMessage{Payload: reply})`.

### Backoff: one vocabulary

**What changed.** Every backoff is one [`bound.Backoff{Initial, Max, Factor}`](../bound/backoff.go) field, resolved by `bound.ResolveBackoff` against a `bound.BackoffRule` and computed by one `ResolvedBackoff.Delay(attempt)`: `Initial` for the first attempt, `Initial × Factor^(attempt-1)` after it, capped at `Max`; a left-out field takes the rule's default, both durations are at least 1ms and never unlimited, and a `Max` below `Initial` and a `Factor` below 1 or NaN are refused naming `<Struct>.Backoff.Initial`, `.Max` or `.Factor`. The fields move as follows:

| Struct | Before | Now |
|---|---|---|
| `messagebus.RetryPolicy` | `BaseDelay`, `MaxDelay` | `Backoff` (1s, 1h, factor 2) |
| `rueidis.RetryConfig`, `mysql.RetryConfig`, `pgsql.RetryConfig` | `InitialDelay`, `MaxDelay`, `BackoffMultiplier` | `Backoff` (500ms, 5s, 2) |
| `rueidis.ReconnectConfig`, `amqp.ReconnectConfig` | `InitialBackoff`, `MaxBackoff`, `BackoffFactor` | `Backoff` (1s, 30s, 2) |
| `outbox.RelayConfig` | `InitialBackoff`, `MaxBackoff`, `BackoffFactor` | `Backoff` (15s, 10m, 2) |
| `outbox.RelayConfig` | `UnavailableBackoff`, `UnavailableBackoffMax`, borrowing `BackoffFactor` | `UnavailableBackoff` (15s, 1m, its own factor 2) |
| `outbox.RelayCommandConfig` | `MaxErrorBackoff` | `ErrorBackoff` (`Initial` left out = the run's `--interval`, capped at `Max`; 1m; 2) |

The consume command's retry grows exponentially: 1s, 2s, 4s… capped at 1h, where it grew linearly, 1s, 2s, 3s…; a `Factor` of 1 is a constant delay. A retry configuration whose `Max` is below its `Initial` is refused at construction, naming both fields, where the rueidis and bunorm retries capped the first wait silently and the consume command's `RetryPolicy` capped every delay at `MaxDelay` silently; a `Backoff` writing only `Max` below the default `Initial`, `RetryPolicy{Backoff: bound.Backoff{Max: bound.Of(500 * time.Millisecond)}}` under the 1s default, is refused naming `RetryPolicy.Backoff.Initial`, a field the application never wrote, and `NewConsumeCommand` panics on it. A `Factor` that is infinite is refused with the one below 1 or NaN. The relay command's first wait after a failure is capped at `ErrorBackoff.Max` when the poll interval exceeds it.

**Symptom.** `unknown field BaseDelay in struct literal of type messagebus.RetryPolicy`, `unknown field InitialDelay in struct literal of type rueidis.RetryConfig`, `unknown field InitialBackoff in struct literal of type amqp.ReconnectConfig`, `unknown field UnavailableBackoffMax in struct literal of type outbox.RelayConfig`, `unknown field MaxErrorBackoff in struct literal of type outbox.RelayCommandConfig`; a consumer retrying a failed message waits 4s before its third retry where it waited 3s.

**Remedy.** Move the values into the struct's `Backoff`: `RetryPolicy{Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(time.Hour)}}`, `RetryConfig{MaxAttempts: bound.Of(10), Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}}`, `ReconnectConfig{Backoff: bound.Backoff{Factor: 1.5}}`, `RelayConfig{UnavailableBackoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}}`, `RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(time.Minute)}}`; write `Factor: 1` on the retry policy to keep a constant delay.

### Opentelemetry: the request metrics follow the semantic conventions

**What changed.** [`NewMetricsMiddleware`](../../integrations/opentelemetry/v4/metrics_middleware.go) records `http.server.request.duration` in seconds (`http_server_request_duration_seconds` in the exposition), bucketed on the conventions' advisory boundaries, `DefaultDurationBoundaries()` answering a fresh copy of them, with `url.scheme`, the scheme the kernel resolved under its forwarded-headers policy, and an integer `http.response.status_code`; the lifecycle decorator records `melody.http.server.lifecycle.duration` the same way, its `url.scheme` read off the connection, since it runs before the kernel resolves one. An empty `DurationBoundaries` list is refused, and `HandlerDecoratorConfig.DurationBoundaries` without a `Meter` is refused naming both. The counters `http.server.request.count` and `http.server.lifecycle.request.count`, and the millisecond histograms `http.server.request.duration` and `http.server.lifecycle.request.duration`, are gone.

**Symptom.** No compile error: dashboards and alerts on `http_server_request_count_total`, `http_server_lifecycle_request_count_total`, `http_server_request_duration_milliseconds` and `http_server_lifecycle_request_duration_milliseconds` go empty, or read wrong by a factor of 1000.

**Remedy.** Read `http_server_request_duration_seconds_count` for the request count and quantiles over `http_server_request_duration_seconds_bucket`; the lifecycle family is `melody_http_server_lifecycle_duration_seconds`. Set `MetricsMiddlewareConfig.DurationBoundaries` or `HandlerDecoratorConfig.DurationBoundaries` to keep buckets of your own.

### Opentelemetry: the Prometheus meter has a close door; the constructors answer an error

**What changed.** [`NewPrometheusMetrics(PrometheusConfig{MeterName})`](../../integrations/opentelemetry/v4/prometheus.go) answers a handle with `Meter()`, `Handler()` and `CloseWithContext(closeContext)`; `ModuleConfig.Prometheus` replaces `ModuleConfig.MetricsHandler`, serves the handle's exposition on `MetricsPath` and registers it so the teardown closes it. `NewPrometheusMeter` and `MetricsHandler` are removed. `NewTracingMiddleware(tracer, TracingMiddlewareConfig{Propagator})` answers `(Middleware, error)`, a nil tracer an error where it panicked; `NewHandlerDecorator(tracer, HandlerDecoratorConfig{Propagator, Meter, DurationBoundaries})` takes the tracer first; `NewMetricsMiddleware(meter, MetricsMiddlewareConfig{DurationBoundaries})`.

**Symptom.** `undefined: opentelemetry.NewPrometheusMeter`, `opentelemetry.MetricsHandler`; `unknown field MetricsHandler in struct literal of type opentelemetry.ModuleConfig`; `unknown field Tracer in struct literal of type opentelemetry.HandlerDecoratorConfig`; `not enough arguments in call to opentelemetry.NewMetricsMiddleware`; `assignment mismatch: 1 variable but opentelemetry.NewTracingMiddleware returns 2 values`.

**Remedy.** `prometheusMetrics, err := opentelemetry.NewPrometheusMetrics(opentelemetry.PrometheusConfig{})`, then `prometheusMetrics.Meter()` for the middlewares and `ModuleConfig{Prometheus: prometheusMetrics, MetricsPath: "/metrics"}`; move the tracer out of `HandlerDecoratorConfig` into the first argument; hand the middlewares their config struct and check the tracing middleware's error.

### Cron: a template answers its ownership line and its user column

**What changed.** [`cron.Template`](../../integrations/cron/v4/template.go) is `Name()`, `Render(entries, options)`, `OwnershipMarker(applicationName string) string` and `RendersUserColumn() bool`; `OwnedTemplate`, `UserColumnTemplate` and the fallback that judged a silent template by a builtin's name are removed. The generator hands the application's cli name in `RenderOptions.ApplicationName` and asks `OwnershipMarker(applicationName)`, so a template embedding a builtin renders the application's line and its `--prune` sweeps, where its sweep was refused. A name carrying a control character is refused with `cron.ErrInvalidApplicationName` before anything renders, by the generator and by the builtins' `Render`, `cron.Render` included; `cron.ValidateApplicationName` is the check for a custom dialect.

**Symptom.** `*T does not implement cron.Template (missing method OwnershipMarker)`; `undefined: cron.OwnedTemplate`, `cron.UserColumnTemplate`; `wrong type for method OwnershipMarker`.

**Remedy.** Give the template `OwnershipMarker(applicationName string) string`, answering `""` for a dialect never pruned, and `RendersUserColumn() bool`; render `OwnershipMarker(options.ApplicationName)` after `cron.ValidateApplicationName(options.ApplicationName)` and drop a name field the template carried for its line.

### Cron: the module takes a configuration factory and the templates

**What changed.** [`cron.ModuleConfig`](../../integrations/cron/v4/module.go) is `{Configuration func(kernelInstance kernelcontract.Kernel) *Configuration, Templates []Template, KeyDefaults, Runner}`: the eager `Configuration *Configuration` and `ConfigurationFactory` are one factory field, a factory answering nil refused naming `ModuleConfig.Configuration`; `Templates` installs templates after the builtins, a nil entry refused naming its index and templates without a configuration refused. `NewGenerateCommand` and `cron.EnvironmentKeys` are unexported, and `GenerateCommand.RegisterTemplate` and `cron.Commands` are removed: the module is the one door, building the generator and declaring the keys.

**Symptom.** `unknown field ConfigurationFactory in struct literal of type cron.ModuleConfig`; `cannot use configuration (variable of type *cron.Configuration) as func(kernelInstance contract.Kernel) *cron.Configuration value`; `undefined: cron.NewGenerateCommand`, `cron.EnvironmentKeys`, `cron.Commands`; `generateCommand.RegisterTemplate undefined`.

**Remedy.** Rename `ConfigurationFactory` to `Configuration`, or wrap an eager configuration in `func(kernelInstance kernelcontract.Kernel) *cron.Configuration { return configuration }`; register the module instead of building the generator by hand, with the custom dialects in `ModuleConfig.Templates`, and drop the hand-written key declarations.

### Outbox: the repository releases claims, requeues dead rows and prunes sent ones

**What changed.** [`outbox.Repository`](../../integrations/outbox/v4/repository.go)'s six doors take the runtime they run under first, where they took a `context.Context`, and the contract gains `ReleaseClaims(runtimeInstance, ids, claimToken) (int, error)`, `RequeueDead(runtimeInstance, RequeueDeadOptions{Ids, Limit}) (int, error)` and `PruneSent(runtimeInstance, PruneSentOptions{SentBefore, Limit}) (int, error)`, implemented by `*Store` and the lazy repository. A batch a repository failure, a failed hold write or the run's cancellation ended hands back the rows after the one it stopped at through `ReleaseClaims`, where they waited out `VisibilityTimeout`; the row it stopped at still waits it out, so a row published before its `MarkSent` failed is not published twice. `ReleaseClaims` refuses an empty token and `RequeueDead` more `Ids` than its limit.

**Symptom.** A custom repository: `*R does not implement outbox.Repository (missing method ReleaseClaims)` and `(wrong type for method ClaimDueMessages)`; a caller of the store: `cannot use ctx (variable of interface type context.Context) as contract.Runtime value in argument to store.MarkSent`.

**Remedy.** Take `runtimeInstance containercontract.Runtime` first and read `runtimeInstance.Context()`; add the three doors, a release fenced by the claim token on the named in-flight rows. A caller holding a context builds its runtime with `runtime.NewRun(ctx, scope)` or derives one with `runtime.WithContext(runtimeInstance, ctx)`.

### Outbox: the table records the send time, and the prune judges it

**What changed.** The outbox table gains `sent_at`, the time [`MarkSent`](../../integrations/outbox/v4/store.go) resolves a row as sent, and `PruneSent` deletes the rows sent before `PruneSentOptions.SentBefore`, where it judged the enqueue time `created_at`. A sent row with no send time is never pruned. `EnsureSchema` adds the column to a table created without it.

**Symptom.** On a table managed by migrations and not altered, `Enqueue` fails first, inside the business transaction that calls it, then `ClaimDueMessages`, each with the database's unknown-column error, since the row model names `sent_at` in the insert and the claim's select.

**Remedy.** Alter the table BEFORE the new binary is deployed, so no business write meets the missing column: on PostgreSQL `ALTER TABLE melody_outbox ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL;` and on MySQL `ALTER TABLE melody_outbox ADD COLUMN sent_at DATETIME NULL;`, the type bun gives the table's other time columns there, then `UPDATE melody_outbox SET sent_at = created_at WHERE status = 'sent' AND sent_at IS NULL;`.

### Outbox: the module takes factories; one relay command constructor

**What changed.** [`outbox.ModuleConfig`](../../integrations/outbox/v4/module.go) is `{Store, Relay, RelayCommand}`, where `Store` and `Relay` are the providers of `service.outbox.store` and `service.outbox.relay`, `func(resolver containercontract.Resolver) (*outbox.Store, error)` and `(*outbox.Relay, error)`, run at first resolution; the prebuilt `Store *Store` and `Relay *Relay`, `StoreFactory`, `RelayFactory` and the two "set exactly one" refusals are removed, and a nil `Relay` registers no relay and no command. [`NewRelayCommand(config)`](../../integrations/outbox/v4/relay_command.go) is the one constructor of `melody:outbox:relay`: it resolves `service.outbox.relay` from the run's scope at the first batch and keeps it for the run. `NewRelayCommandFromResolver`, `RegisterStoreService`, `RegisterRelayService` and the `outbox.ServiceRegistrar` interface are removed.

**Symptom.** `unknown field StoreFactory in struct literal of type outbox.ModuleConfig` (and `RelayFactory`); `cannot use store (variable of type *outbox.Store) as func(resolver contract.Resolver) (*outbox.Store, error) value`; `too many arguments in call to outbox.NewRelayCommand`; `undefined: outbox.NewRelayCommandFromResolver`, `outbox.RegisterStoreService`, `outbox.RegisterRelayService`, `outbox.ServiceRegistrar`.

**Remedy.** Rename `StoreFactory` to `Store` and `RelayFactory` to `Relay`; a prebuilt object becomes `func(resolver containercontract.Resolver) (*outbox.Relay, error) { return relay, nil }`. Build the command with `outbox.NewRelayCommand(outbox.RelayCommandConfig{})` and register the relay under `outbox.ServiceRelay`, which the module does; register the store and the relay through the module.

### Bunorm: a provider has one open door per purpose, each taking the runtime

**What changed.** [`bunorm.Provider`](../../integrations/bunorm/v4/provider.go) is `Open(runtimeInstance, parameters, logger)` and `OpenForMigration(runtimeInstance, parameters, logger)`, both answering `(*bun.DB, error)` under the runtime's context. `ContextOpener`, `MigrationContextOpener` and the optional `MigrationProvider` are removed, so the registry probes no capability: every provider carries the migration door, and one with nothing to tune for migrations answers its `Open` there. The `mysql` and `pgsql` providers' `Open` and `OpenForMigration` are the bodies of their removed `OpenContext` and `OpenForMigrationContext`, and they refuse a nil runtime, a typed nil and one carrying no context with "the runtime is nil or carries no context", where a nil context read as `context.Background()`.

**Symptom.** `*P does not implement bunorm.Provider (wrong type for method Open)` or `(missing method OpenForMigration)`; `undefined: bunorm.ContextOpener`, `bunorm.MigrationContextOpener`, `bunorm.MigrationProvider`; `provider.OpenContext undefined`.

**Remedy.** Move the body of `OpenContext` into `Open`, reading the context from `runtimeInstance.Context()`, and delete the context-less twin; a provider with nothing to tune for migrations adds `func (p *P) OpenForMigration(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) { return p.Open(runtimeInstance, parameters, logger) }`. The registry keeps that answer as the name's migration connection, a second pool, unless it is the very `*bun.DB` the registry already holds as the name's pool, which it shares by identity and never closes on a release. A direct call writes `provider.Open(runtimeInstance, parameters, logger)`.

### Bunorm: the registry captures nothing, and its doors take the caller's runtime

**What changed.** [`NewManagerRegistry`](../../integrations/bunorm/v4/manager_registry.go) is `NewManagerRegistry(definitions, ManagerRegistryConfig{Logger})` and captures no context. `Manager`, `MustManager`, `Database`, `MustDatabase`, `DefaultManager`, `MustDefaultManager`, `DefaultDatabase`, `MustDefaultDatabase` and `MigrationDatabase` take the runtime they run under first, and the provider is handed it over a context the caller's own context and the registry's `CloseWithContext` both end; `ReadWriteSplitter.Writer` and `Reader` take it too. `MigrationDatabase` answers `(*bun.DB, error)`, its `dedicated` bool gone. `SetLogger` is removed: the registry reports through the constructor's logger for its whole life. A nil runtime, a typed nil and one carrying no context are refused with "the runtime is nil or carries no context", one carrying no scope with "the runtime carries no scope"; a caller whose context is already done is refused before an open starts, and before `MigrationDatabase` takes a hold on a connection already kept. Concurrent callers of one name coalesce onto the first caller's open: a waiter whose own context ends leaves with its cause, and a waiter whose context is live opens again when the open it waited on ended with its opener's context done, whatever the failure carries. A migration answer that is the name's very pool is shared with it whichever door opened it first, so `CloseMigrationDatabase` never closes the pool the registry serves.

**Symptom.** `too many arguments in call to bunorm.NewManagerRegistry`; `not enough arguments in call to registry.Manager` (and the eight other doors, `splitter.Reader`, `splitter.Writer`); `assignment mismatch: 3 variables but registry.MigrationDatabase returns 2 values`; `registry.SetLogger undefined`. At run time, a lazy open ends when the request or the run that asked for it ends, where it ran under the registry's construction context.

**Remedy.** `NewManagerRegistry(definitions, bunorm.ManagerRegistryConfig{Logger: logger})`, built inside the registry's provider, where the application's logger resolves, instead of at wiring on the emergency logger and re-pointed with `SetLogger`; register that provider with `RegisterOptions{BuiltAtBoot: true, BootFailure: container.BootFailureFailsBoot}`, since the construction opens nothing and its definition refusals then fail the boot; hand the request's or the run's runtime to every door, `registry.Database(runtimeInstance, name)`, `registry.DefaultManager(resolver.Runtime())` inside a provider; `database, migrationErr := registry.MigrationDatabase(runtimeInstance, name)` followed by `registry.CloseMigrationDatabase(name)` on the way out. A caller holding a runtime binds once and calls the doors without it: `boundRegistry := bunorm.FromRuntime(runtimeInstance, serviceName)`, or `bunorm.For(runtimeInstance, registry)` for a registry in hand, then `boundRegistry.Database(name)`, `boundRegistry.MigrationDatabase(name)` and `boundRegistry.Registry().CloseMigrationDatabase(name)`.

### Storage: `Get` takes options and answers the object with its info

**What changed.** [`storagecontract.Storage.Get`](../storage/contract/storage.go) is `Get(runtime, key, storagecontract.GetOptions) (storagecontract.Object, error)`: the object's `Body`, an `io.ReadCloser`, beside its `Info`, a [`storagecontract.ObjectInfo`](../storage/contract/storage.go) of `Size`, `ContentType`, `ModifiedAt`, `EntityTag` without quotes and `Key`, the key asked. Both backends fill the info from the stat they already took, so a read costs no request more. Userland calls the storage through [`storage.Bound`](../storage/bound.go), `storage.FromRuntime(runtimeInstance)` or `storage.For(runtimeInstance, storageInstance)`, whose doors take no runtime.

**Symptom.** `not enough arguments in call to store.Get`, the options being new; once they are passed, `cannot use reader (variable of struct type storagecontract.Object) as io.Reader value` or `reader.Close undefined (type storagecontract.Object has no field or method Close)`; an implementation of the contract: `does not implement storagecontract.Storage (wrong type for method Get)`.

**Remedy.** `object, getErr := storage.FromRuntime(runtimeInstance).Get(key, storagecontract.GetOptions{})`, read and close `object.Body`, exactly once, also after a failed read; beside an error the object is the zero value. An implementation answers `storagecontract.Object{Body: reader, Info: info}` and the zero `Object` beside every error, and honours `options.Range`: it refuses a negative range through `storagecontract.RefuseNegativeByteRange(options.Range)` before it asks its store, and reads the range through `storagecontract.ResolveByteRange(options.Range, info.Size)`, which answers the offset and the clipped length or the refusal, `ErrRangeNotSatisfiable` for an offset at or past the end; a backend that ignores the range answers the whole object to a caller that asked a part.

### Storage: `Stat` is part of the contract, and `Exists` is a function of the package

**What changed.** [`Storage.Stat(runtime, key) (storagecontract.ObjectInfo, error)`](../storage/contract/storage.go) answers what the storage knows about the object without opening it: it refuses what `Get` refuses, by the same causes, and answers the info a `Get` would answer at that instant. The S3 storage sends one `HEAD`; `LocalStorage` judges the file without opening it, so a named pipe answers at once. `Exists` leaves the contract: [`storage.Exists(boundStorage, key)`](../storage/exists.go) answers `true` when `Stat` answers, `false` when it answers `ErrObjectNotFound` or `ErrKeyIsDirectory`, and `Stat`'s error otherwise. A named pipe or a device under a local key answers `Stat`'s refusal, `not a regular object`, where `LocalStorage.Exists` answered `false, nil`.

**Symptom.** An implementation of the contract: `missing method Stat`; a caller: `store.Exists undefined (type storagecontract.Storage has no field or method Exists)`.

**Remedy.** Implement `Stat` by the rule "refuses what `Get` refuses": share one function between the two doors that judges what was found, and delete the implementation's `Exists`. Call `storage.Exists(storage.For(runtimeInstance, store), key)`, or `storage.Exists(boundStorage, key)` over a view already bound; a caller that asked `Exists` before reading the size or the content type asks `Stat`.

### Storage: an absent object is `ErrObjectNotFound`

**What changed.** `Get` and `Stat` of a key that names no object answer an error whose chain carries [`storagecontract.ErrObjectNotFound`](../storage/contract/error.go), on both backends: locally a key never stored, a key under a regular file and a base directory that is gone; on S3 `NoSuchKey`. A refusal of the key, a directory, a 403 or a transport failure never carries it. `Delete` of an absent key answers `nil`, as it did, now the stated rule.

**Symptom.** None at compile time. A caller that matched the sentence `object storage object not found`, or `fs.ErrNotExist` in the local chain, keeps working; one that called `Exists` and then `Get` pays two round trips where one answers.

**Remedy.** `exception.ChainIs(getErr, storagecontract.ErrObjectNotFound)`; the `Exists`-then-`Get` pair becomes one `Get`.

### Storage: the local backend keeps the content type

**What changed.** [`LocalStorage`](../storage/local.go) keeps the content type a `Put` declares in a metadata object beside the object, `.melody-storage-<sha256 of the key's leaf>.metadata`, and answers it in `ObjectInfo.ContentType` at `Get` and `Stat`, honoured only while the object keeps the size and the modification time of the write it describes. A `Put` that declares none removes it, `Delete` removes it, and a key spelled like a metadata object is refused at every door.

**Symptom.** A `.melody-storage-….metadata` file beside an object stored with a content type; a tool that lists the base directory sees it. An object stored before the upgrade answers an empty content type.

**Remedy.** None, or skip the reserved `.melody-storage-` prefix in a tool that lists the directory; an object answers its content type once stored again.

### Storage: `PresignedUrl` takes a config

**What changed.** [`Storage.PresignedUrl(runtime, key, storagecontract.PresignConfig)`](../storage/contract/storage.go) takes `PresignConfig{Expiry bound.Duration, Method string}`. `Expiry` is read against [`storagecontract.PresignExpiryRule()`](../storage/contract/presign.go): 15 minutes when left out, at least 1s, at most 7 days, unlimited refused, each refusal naming `PresignConfig.Expiry` before the backend signs. `Method` is `http.MethodGet` when left out; `http.MethodPut` signs an upload of the key, which binds neither a content type nor a size; any other method is refused with [`storagecontract.ErrPresignUnsupported`](../storage/contract/error.go). `LocalStorage` refuses every presign with the same cause, its message kept.

**Symptom.** `cannot use 15 * time.Minute (constant 900000000000 of type time.Duration) as storagecontract.PresignConfig value in argument`; at run time an expiry of `0`, refused by minio before, signs for 15 minutes, and one past 7 days is refused by the framework naming the field, where minio refused it with its own sentence.

**Remedy.** `storagecontract.PresignConfig{Expiry: bound.Of(15 * time.Minute)}`, with `Method: http.MethodPut` for an upload link. A caller that falls back to streaming on the local backend branches on `exception.ChainIs(presignErr, storagecontract.ErrPresignUnsupported)`; a backend of the application's own reads the expiry through `storagecontract.ResolvePresignExpiry` and the method through `storagecontract.ResolvePresignMethod`.

### Cache: the callback of `Remember` receives the flight's runtime and an item, and chooses its ttl or skips the store

**What changed.** The callback of [`Remember`](../cache/remember.go) is `func(runtimeInstance containercontract.Runtime, item *cache.RememberItem) (T, error)`. The runtime is the flight's, never a caller's: a run of its own, `KindRun`, over the flight's context, in a scope the flight opens behind the leader's and closes once it completed, so the callback resolves its services whichever request ends first; a leader whose runtime opens no child run, the boot runtime, a provider's runtime or a scope of another implementation, lends its own scope over the flight's context, and a view kept past its closed scope is answered that scope's error; with no flight it is the caller's own runtime. Through the item it chooses the ttl of the entry it computes, `item.SetTtl(bound.Of(time.Minute))`, or asks that the value is answered and not stored, `item.SkipStore()`. `RememberConfig.Ttl` stays required and is what an untouched item stores under.

**Symptom.** A callback declared `func(ctx context.Context, _ *cache.RememberItem) (T, error)` no longer compiles: `in call to cache.Remember, type func(ctx context.Context, _ *cache.RememberItem) (string, error) of func(ctx context.Context, _ *cache.RememberItem) (string, error) {…} does not match func(runtimeInstance "github.com/precision-soft/melody/v4/container/contract".Runtime, item *cache.RememberItem) (T, error) (cannot infer T)`, or, where the call names its type, `cannot use func(ctx context.Context, _ *cache.RememberItem) (string, error) {…} (value of type func(ctx context.Context, _ *cache.RememberItem) (string, error)) as func(runtimeInstance "github.com/precision-soft/melody/v4/container/contract".Runtime, item *cache.RememberItem) (string, error) value in argument to cache.Remember[string]`.

**Remedy.** Take the runtime and read its context where a context was read: `func(flightRuntime containercontract.Runtime, _ *cache.RememberItem) (T, error)`, `repository.All(flightRuntime.Context())`; resolve a service the computation needs through it, `runtime.FromRuntime[T](flightRuntime, name)`, never through the caller's runtime the callback captures. A helper that stored an absence under one ttl and wrote a found value again under another sets the ttl in the callback and drops its second `Set`. A ttl below a second chosen in the callback is answered by `Remember` as an error; `bound.Of(0)` does not mean "do not store", `item.SkipStore()` does.

### Runtime: the runtime lives under the container, names its phase, and reaches no container

**What changed.** The `Runtime` contract moved from `v4/runtime/contract` to [`v4/container/contract`](../container/contract/runtime.go), and its implementation, with `FromRuntime` and `MustFromRuntime`, from `v4/runtime` to [`v4/container/runtime`](../container/runtime); the old packages are gone. `Runtime` gains `Kind()`, the phase it stands for, and loses `Container()`: a runtime resolves through its scope, which answers the container's own services as before. `runtime.New(ctx, scope, container)` is replaced by `NewRequest(ctx, scope)` and `NewRun(ctx, scope)`; a runtime over a derived context is `WithContext(runtime, ctx)`, which keeps the kind; the scope of one delivery or one tick inside a run is `NewChildRun(parent, ctx)`. `debug:container` is built over the container it describes, `debug.NewContainerCommand(container)`.

**Symptom.** `no required module provides package github.com/precision-soft/melody/v4/runtime`; `undefined: runtime.New`; `runtimeInstance.Container undefined`; a hand-written runtime `does not implement Runtime (missing method Kind)`; `&debug.ContainerCommand{}` describes nothing.

**Remedy.** Import `github.com/precision-soft/melody/v4/container/runtime` and read `Runtime` from `containercontract`; build a handler's or a listener's test runtime with `runtime.NewRequest(ctx, scope)`, a command's with `runtime.NewRun(ctx, scope)`; replace `runtime.New(ctx, rt.Scope(), rt.Container())` with `runtime.WithContext(rt, ctx)` and `rt.Container().NewScope()` with `runtime.NewChildRun(rt, ctx)`, closing its scope; resolve through `rt.Scope()` where the code read `rt.Container()`, and keep a container the code registers on or describes beside the runtime, where it was built; a hand-written runtime answers `Kind()`, or is replaced by a constructor; `debug.NewContainerCommand(kernel.ServiceContainer())`.

### Application: a service built at boot is declared on its registration, and the pipeline is built inside `Boot`

**What changed.** [`RegisterOptions.BuiltAtBoot`](../container/contract/registrar.go) has the application build a container service inside `Boot`, under the boot runtime, in registration order, and `RegisterOptions.BootFailure` says what a provider that fails does to the boot: the zero value, `BootFailureWarns`, writes the boot warning `application.service.builtAtBootFailed` and has the first resolution build the service again, a cli process building it at its first use; `BootFailureFailsBoot` fails the boot naming the service. A refusal of the container's wiring fails the boot in either mode. In an http process the session manager's storage timeout is judged and the middleware pipeline built inside `Boot`, no longer in `Run`.

**Symptom.** A service resolved after `Boot` in `main` to have it built before the first request is built under no runtime of the boot; a refusal of the session storage timeout or of a middleware factory now exits from `Boot`; a test that called `Boot` and served the application's handler without `Run` now gets the pipeline; a scoped registration with `BuiltAtBoot` is refused with `a scoped service is not built at boot`; a service registered `BuiltAtBoot: true` whose provider fails boots with the warning instead of failing the boot; a mode without `BuiltAtBoot` is refused with `a boot failure mode needs a service built at boot`.

**Remedy.** Register the service with `RegisterOptions{BuiltAtBoot: true}` and drop the resolution after `Boot`. Where the failure can only be configuration, add `BootFailure: container.BootFailureFailsBoot` to keep the boot failing; where it is the reachability of something outside the process, leave the default and put the service on the resolution path of what uses it, so the first use after the outage builds it, with a provider safe to run more than once. A factory that needs the boot runtime reads `kernelInstance.BootRuntime()` while it builds; the `--build` inspection of `debug:middleware` runs after `Boot` and reports `ErrBootFinished` for such a factory.

### Kernel: a boot hook resolves under the boot runtime, and the phase lookups take the runtime

**What changed.** [`Kernel`](../kernel/contract/kernel.go) declares `BootRuntime()` in place of `Context()`: the runtime of the application's boot, done with `containercontract.ErrBootFinished` once `Boot` returned. `kernel.NewKernel` no longer takes a context. `ScopeManager` declares `NewBootScope()` and `Registrar` declares `ReservePhaseService(name, kind)`. `http.RequestContextFromRuntime` / `RequestContextMustFromRuntime` and `application.ProcessContextFromRuntime` / `ProcessContextMustFromRuntime` replace the `…FromResolver` forms and answer an error for a runtime of another kind.

**Symptom.** `kernelInstance.Context undefined`; `too many arguments in call to kernel.NewKernel`; `undefined: http.RequestContextFromResolver`, `undefined: application.ProcessContextFromResolver`; a registrar of your own `does not implement Registrar (missing method ReservePhaseService)`; a scope manager of your own `missing method NewBootScope`; `context canceled` with the cause `the application boot has finished`; `a scoped service is not built at boot`.

**Remedy.** In a boot hook resolve under `kernelInstance.BootRuntime()`: `config.ConfigMustFromRuntime(kernelInstance.BootRuntime())`, or `runtime.MustFromRuntime[T](kernelInstance.BootRuntime(), name)`. For a context that must outlive the boot, keep the context you gave `NewApplication`. A value that keeps the boot runtime and uses it after `Boot` gets `ErrBootFinished`: take the runtime of the request or the run. Read the request context with `http.RequestContextFromRuntime(runtimeInstance)` and the process context with `application.ProcessContextFromRuntime(runtimeInstance)`, and treat an error under `ErrRuntimeKindRefused` as absence where the tolerant form answered nil. A registrar double declares `ReservePhaseService` and may answer nil. A scoped service asked at boot is built in the request or the run that needs it.

### Container: a provider reads the runtime of its resolution through `resolver.Runtime()`

**What changed.** [`Resolver`](../container/contract/resolver.go) declares `Runtime()`, the runtime of the resolution in flight: inside a provider, the context of the call that started the creation, the kind of the scope that call entered through, and the provider's own scope, or a suspended scope for a container-owned provider; on the container, a scope and a resolver retained after its provider returned, a detached runtime over `context.Background()`, never nil. `runtime.NewRequest` and `runtime.NewRun` bind their kind to the scope, so one scope serves one kind. `container.ContextOf` and `container/contract.ResolutionContext` are gone.

**Symptom.** `undefined: container.ContextOf`; `undefined: containercontract.ResolutionContext`; a resolver of your own `does not implement Resolver (missing method Runtime)`; a test that builds `runtime.NewRequest` and `runtime.NewRun` over one scope panics with `scope cannot serve the runtime`.

**Remedy.** Write `resolver.Runtime().Context()` where you wrote `container.ContextOf(resolver)`; regenerate the wiring with `go generate ./generated`. A resolver of your own declares `Runtime()`; a test double may answer nil, which the container never reads. Give each runtime of another kind its own scope. A singleton reads the kind of the call that created it first, and a call through the container itself is `KindDetached`: resolve through `runtimeInstance.Scope()` to hand a provider the phase. Inside a provider resolve through the resolver or its runtime, whose scope carries the provider's chain so a cycle is refused as circular; a container or a scope held from outside the provider starts a chain of its own and waits on its own creation until its context ends.

### Config: a key is read as `%env(KEY)%`

**What changed.** A `.env` key is no longer a parameter. It was registered as a second name of the parameter it fed, and a line nobody declared as a parameter of its own name; now the configuration's parameter table holds parameter names only, and a template reads a key as `%env(KEY)%`. A `%KEY%` reference to a loaded or declared key is refused with its own message. `MarkSecret` and [`Application.MarkParameterSecret`](../application/application.go) take a parameter name; a `MELODY_*` key nobody declared marks its readers at the boot resolve. `debug:parameters` lists parameters only and drops the `aliases` column. [`EnvironmentKeyDeclarer`](../config/contract/environment_key.go) gains `EnvironmentValue(key)`, the loaded text of a line, which the `application.environment.processVariableIgnored` warning compares for a key nobody declared. A declared key is upper-case letters, digits and underscores, so a lower-case or dotted line (`database_url`, `db.host`) is read through `%env(database_url)%` or `%env(db.host)%` and cannot be declared until it is renamed.

**Symptom.** The boot exits with `a template references an environment key as a parameter; a key is read as %env(KEY)%`, naming the parameter, the key and the offset; `configuration.Get("APP_X")` answers nil and `MustGet` panics `parameter does not exist`; `MarkParameterSecret("APP_X")` ends as the boot warning `application.secretMark.unmatched`; a reader of `aliases` in the json of `debug:parameters` finds nothing; an out-of-tree `EnvironmentKeyDeclarer` no longer compiles.

**Remedy.** Declare the key in `ApplicationConfig.EnvironmentKeys` and read its parameter, or write `%env(APP_X)%`; declare a credential key `Secret`; rename a lower-case line to declare it; implement `EnvironmentValue`.

### Config: `%env(default:…)%` is gone

**What changed.** The `default:` processor of `%env(…)%` is removed; `%env(KEY)%` is the only form, and a key's fallback is the `Default` of its declaration.

**Symptom.** `malformed environment placeholder in template; the only form is %env(KEY)%; a key's fallback is the Default of its EnvironmentKey declaration, …`.

**Remedy.** Move the fallback into the key's `EnvironmentKey.Default`; for a fallback that is another parameter, write that parameter's reference as the default.

### Config: a framework or module parameter is overridable, and its key then refuses

**What changed.** [`ApplicationConfig.ParameterOverrides`](../application/application_new.go) rewrites, by parameter name, the template of a `kernel.*` parameter or of a parameter a module's key feeds, and the override is the parameter's one source: the key that fed it is no longer read, so a `.env` artifact that still sets it refuses the boot, unless the override reads it. A `kernel.*` override is resolved when the application is built and reads keys, literals and other `kernel.*` parameters only; `kernel.environment` and `kernel.project_directory` are not overridable. [`Parameter`](../config/contract/parameter.go) gains `IsOverridden()`, and [`EnvironmentKeyDeclarer`](../config/contract/environment_key.go) gains `OverriddenEnvironmentKeys`, `UnappliedParameterOverrides` and `EnvironmentKeyParameter`.

**Symptom.** `the environment key … is set and reads nothing: the application overrides …; remove the line`; `a parameter override names a parameter no framework or module key feeds`; an out-of-tree `configcontract.Parameter` or `EnvironmentKeyDeclarer` no longer compiles.

**Remedy.** Remove the line, or the override; name a parameter a framework or module key feeds; implement the new methods.

### Config: `NewConfiguration` takes a `ConfigurationConfig`, and an application declares its keys

**What changed.** [`config.NewConfiguration`](../config/configuration.go) takes a third argument, a [`ConfigurationConfig`](../config/configuration.go), which carries the application's keys; [`ApplicationConfig.EnvironmentKeys`](../application/application_new.go) hands them to it, so an application declares its `.env` keys as a module does, with a default, a rule, a secret mark and retired spellings, before any parameter is read. An application's key is any name of upper-case letters, digits and underscores starting with a letter outside the `MELODY_` namespace, which is refused, and may retire a `MELODY_` spelling; a module's key stays in the namespace, and a module may not be named `the framework` or `the application`. The declaration refusals name no module: the declaring owner is under the context key `owner`, the owner of a colliding key under `holder`, and a value a declared key's rule refuses after the resolve fails the boot with `a declared environment key is refused`.

**Symptom.** `not enough arguments in call to config.NewConfiguration`; `module name may not be an owner name the framework reserves`; a test matching the text `a module's environment key …` or `a module declared …`, or reading the context key `module` of a declaration refusal or `owner` as the colliding key's owner, no longer matches.

**Remedy.** Pass `config.ConfigurationConfig{}`; rename a module named `the framework` or `the application`; declare the application's keys in `ApplicationConfig.EnvironmentKeys` instead of reading `.env` names by hand; match the new texts, read the declaring owner under `owner` and the colliding one under `holder`.

### Config: `Parameter.EnvironmentValue()` is `Template()`, and `%env(KEY)%` reads the declared default

**What changed.** A key and the parameter it feeds are two tables of the configuration: the parameter holds the template `%env(KEY)%`, and `%env(KEY)%` reads the artifact's line when one sets the key, a blank line under a declared key's rule counting as unset, and the key's declared default otherwise, the framework's 33 keys included; it refused a key no artifact set even when the framework or a module declared a default. [`Parameter.EnvironmentValue()`](../config/contract/parameter.go) is renamed `Template()` and answers the template, `%env(KEY)%` for a keyed parameter where it answered the raw `.env` text or the default. A framework default is text: the four defaults that were a number or a boolean answer it as a string through `Value()`, as a value an artifact sets always did. A module key is resolved when it is declared. `debug:parameters` names the column `template`, in the table and in the json.

**Symptom.** `EnvironmentValue undefined`; an out-of-tree `configcontract.Parameter` no longer compiles; `Value().(int)` or `Value().(bool)` on a defaulted framework parameter panics; a reader of `environmentValue` in the json of `debug:parameters` finds nothing.

**Remedy.** Call `Template()`; read a number, a switch or a duration through `Int()`, `Bool()` or `Duration()`; read `template` in the json.

### Config: a retired key refuses the boot

**What changed.** A `.env` artifact that still sets a key the framework renamed refuses the boot naming its successor: `MELODY_PUBLIC_DIR` names `MELODY_PUBLIC_DIRECTORY`. A module retires a spelling of its own through [`EnvironmentKey.Retired`](../config/contract/environment_key.go), and the cron module retires `MELODY_CRON_LOGS_DIR`, a spelling of the unreleased fourth major, for `MELODY_CRON_LOGS_DIRECTORY`. [`Boot`](../application/application.go) checks right after the modules declare their keys, before any other module hook and before the resolve, in http and cli alike, the boot warnings disabled or not, the artifacts the process loads: `.env`, `.env.local` and the selected environment's two files. The line's presence refuses: an empty value, and the successor set beside it, refuse too. Every retired key is named in one refusal, so a deployment is corrected in one pass, and the refusal never carries a value. The cron module also refuses a parameter the application still registers as `melody.cron.logs_dir`, naming `melody.cron.logs_directory`, after the resolve. A process environment variable of a retired name is named with its successor in the `application.environment.processVariableIgnored` boot warning. There is no door: the key's meaning is certain, nothing reads it, and its silent default was the misconfiguration. An out-of-tree implementation of [`EnvironmentKeyDeclarer`](../config/contract/environment_key.go) gains `RetiredEnvironmentKeys` and `RetiredEnvironmentKeySuccessor`.

**Symptom.** The boot exits with `the environment key MELODY_PUBLIC_DIR is retired: rename it to MELODY_PUBLIC_DIRECTORY`, several keys joined with `; ` in key order, where it booted on the default directory with the `application.environment.undeclaredKey` warning. The boot exits, in every process shape, with `the parameter melody.cron.logs_dir is retired: rename it to melody.cron.logs_directory`; a value still naming `%kernel.logs_dir%`, the third major's default, is refused first by the resolve as an undefined parameter, so that migration takes two boots. A configuration of the application's own that implements `EnvironmentKeyDeclarer` no longer compiles. The retired key of a module the process does not register stays the undeclared-key warning: the framework cannot know a rename nobody declared.

**Remedy.** Rename the key in every `.env` artifact, the per-environment and embedded ones included, and the parameter where the application registers it; implement `RetiredEnvironmentKeys` and `RetiredEnvironmentKeySuccessor` on a configuration of the application's own.

### Security: a regex access control rule beside a `"/"` prefix rule is refused

**What changed.** Access control resolves every prefix rule ahead of every regex rule, and a `"/"` prefix rule, segment or raw, claims every path. [`accesscontrol.NewControl`](../security/accesscontrol/control.go) now refuses a control holding both, where the regex rule was accepted and never decided. See [the match priority](package/SECURITY.md).

**Symptom.** The boot panics with `access control regex rule never decides beside a "/" prefix rule`, naming `ruleIndex`, `pattern` and `prefixRuleIndex`. The pair can come from one list or from a firewall's rules merged with the global ones.

**Remedy.** Declare the catch-all as the empty raw prefix, `NewRawPrefixRule("", ...)`, or as a last `^/` regex rule, both of which answer only what no other rule claimed. Read the regex rules again before the release: each one starts deciding the paths it names.

### Security: a firewall serves the sign-in and sign-out doors it declares

**What changed.** The third major validated `LoginPath`, `LogoutPath`, `LoginHandler` and `LogoutHandler` and consumed none of them: a POST on the login path reached the router. A firewall now serves them on `kernel.request`, before access control. [`FirewallConfig`](../security/config/security_module.go) carries a [`LoginConfig`](../security/config/login_config.go) and a [`LogoutConfig`](../security/config/login_config.go), each declared by `Path` and `Handler` on its own, with `Methods` (`POST`), `TargetPath` (`/`), `CsrfDisabled` and `Name`, `Attributes`; `LoginConfig` also has `Authenticator`, `Limiter`, `LimiterConfig`. `LoginInput` gains `PreviousSessionId`, `LogoutInput` gains `Token`. See [the doors](package/SECURITY.md#sign-in-and-sign-out-doors).

**Symptom.** `FirewallConfig{LoginPath: ...}` no longer compiles. Any route on a door's path, a `GET` page included, a parametrized or catch-all route too, fails the boot in the collision report, as a `firewallDoorRoute`, or as an `httpRoute` duplicate when the route has the door's very path and methods. A door outside its firewall's prefix, or inside an earlier firewall's, panics at boot. A `GET` on a door's path no longer reaches a handler, so a sign-out link answers 404 and a cross-site POST answers 403.

**Remedy.** Write `Login: securityconfig.LoginConfig{Path: "/login/check", Handler: handler}` and `Logout: securityconfig.LogoutConfig{Path: "/logout", Handler: handler}`. Move a page off the door's path and delete the routes that signed in by hand, or drop the block and keep the routes. Sign out with a POST form. Move the credential reading into an [`Authenticator`](../security/contract/authenticator.go), which rotates the session for the handler: a handler that also rotates loses `PreviousSessionId`'s row, so it reads the field instead.

### Http: one error mechanism, `kernel.exception` listeners; the error handler slot is gone

**What changed.** `Kernel.SetErrorHandler`, `Kernel.HasErrorHandler`, `KernelConfig.ErrorHandler`, `httpcontract.ErrorHandler`, `http.RegisterKernelExceptionListener` and `http.KernelExceptionListenerPriority` are removed. The kernel installs a built-in `kernel.exception` listener when `ServeHttp` builds the handler, at `http.BuiltInExceptionListenerPriority`, the lowest priority; `KernelExceptionEvent.SetResponse` ends the dispatch, so the first responder wins; a listener registered at that priority once the built-in is installed is refused by name. When a listener fails or panics, the kernel renders the error with its own status and does not serve a response the listener had set, which it closes and journals by status and `Content-Type`. `ServeHttp` resolves the event dispatcher when it builds the handler, not on the first request, so a container without one panics there.

**Symptom.** A call of a removed door no longer compiles. A `kernel.exception` listener below one that answers no longer runs, where it used to run and could overwrite the response. A listener at `math.MinInt` registered after the handler was built panics.

**Remedy.** Register a listener with `AddListener(kernelcontract.EventKernelException, listener, eventcontract.ListenerConfig{})` that calls `SetResponse`, or declines by not calling it; call [`RenderExceptionResponse`](../http/exception_listener.go) to delegate to the framework's body. An observer sits at a higher priority than any responder. The handler's `writer` argument has no replacement: a listener answers with a `Response` the kernel writes, whose body may be a stream through `SetBodyReader`, and an error path that needs the raw connection has to act in the handler that failed.

### Http: a `kernel.request` listener refuses through `KernelRequestEvent.Refuse`

**What changed.** `KernelRequestEvent` gains `Refuse(err)` and `Refusal()`: the handler never runs and the kernel raises the error through `kernel.exception`, filing its record before the dispatch, so the record does not depend on who answers. The first refusal is the decision; a refusal stands over a later listener's failure, and a skipped required listener still ends in the fail-closed 500. A listener that both answers and refuses, in either order, panics by name and the request gets that 500, and `Refuse` with a nil or typed-nil error panics by name too. The rate-limit listener refuses through it when `OnLimitExceeded` returns an error; a limit handler that answers a response keeps its response.

**Symptom.** A `kernel.request` listener that calls `SetResponse` without reading `Refusal()` answers a 500 for a request an earlier listener refused. A rate-limit error reaches `kernel.exception` listeners through the kernel instead of a dispatch of the listener's own, and a refused preflight is decorated by the cors response listener only.

**Remedy.** A `kernel.request` listener that answers stands aside on `nil != Refusal()` as it does on `nil != Response()`. A listener that dispatched `kernel.exception` by hand calls `Refuse` instead.

### Security: the firewall's and access control's refusals reach the application's `kernel.exception` listeners

**What changed.** A failed firewall check, a token source error, a failing entry point, a missing decision manager, a denied decision, and the 401 for a missing security context, a missing token or a silent entry point are refusals: the listeners call `Refuse`, so an application's `kernel.exception` listener sees and may answer them. The answers of the firewall's own entry point and access denied handler stay responses, and the listeners read a refusal as they read a response and stand aside. The three 401 are rendered by the built-in listener where they were a hand-written json body: the status is the same, the body is negotiated, so a client preferring html gets the html page, and each now files a warning record carrying its `reason`, as every other deliberate 4xx does.

**Symptom.** A `kernel.exception` listener that answers every error now answers the security refusals too, where they were rendered past it; a listener answering 200 to one is fail-open by its own hand. A journal reading gains one warning per anonymous request refused without an entry point's answer. A test dispatching `kernel.request` by hand reads `Refusal()`, where it read `Response()`.

**Remedy.** A listener that must not touch them inspects the error's status and declines by not calling `SetResponse`.

### Security: a store that cannot answer is 503 with `Retry-After` on a protected route

**What changed.** A token source answers an unavailable token, one carrying [`securitycontract.AuthenticationUnavailable`](../security/contract/token.go), when a credential was presented and the store or remote its check stands on could not answer: the bearer source for a marked validator or enricher error (the opaque store's lookup, the revocation epoch store), the HMAC source for its nonce guard, a full in-process guard and a custom secret provider's short secret, the impersonation decorator for a marked resolver error. A `Resolve` error carrying [`exception.MarkInfrastructureFailure`](../exception/infrastructure_failure.go) is read the same way, unless an http status answers outside what the mark wraps, which is kept; a panic is not. On a protected route access control refuses it with `503 service unavailable` and `Retry-After`, the firewall's `UnavailableRetryAfter` (5s, 1s to 1h in whole seconds), raises `security.authorization.denied` with reason `authentication_unavailable`, puts [`security.ErrAuthenticationUnavailable`](../security/unavailable_token.go) in the refusal's chain and does not consult the entry point; a sign-in door without an `Authenticator` refuses it the same way before its handler. On a `PUBLIC_ACCESS` route, or one no rule claims, the request is served. The three source records are gone: the resolution listener files one Error record per request, naming the firewall, the source, the method and the path. See [a store that cannot answer](package/SECURITY.md#a-store-that-cannot-answer).

**Symptom.** During a store outage a caller presenting a credential on a protected route gets `503` with `Retry-After` where it got the entry point's `401` or login redirect. A `kernel.exception` listener that answers every `401` no longer sees these; one that builds its own response serves the `503` without `Retry-After`. A journal reading `bearer token validation infrastructure failed`, `bearer token enrichment infrastructure failed`, `internal-auth verification infrastructure failed` or `switch-user target resolution infrastructure failed` reads `security token source could not verify the credential: its platform is unavailable` instead. A source returning a marked error from `Resolve` no longer answers 500 on every route. A custom source that swallows an outage to anonymous keeps answering `401`.

**Remedy.** Copy the delay in a listener that builds its own response: `http.SetRetryAfter(response, httpException.RetryAfter())`. Recognise the refusal with `errors.Is(err, security.ErrAuthenticationUnavailable)`. In a custom source or a `TokenResolver`, answer `security.NewUnavailableToken(err)` for an outage, `return security.NewUnavailableToken(err), nil`, or return the error through `exception.MarkInfrastructureFailure`. A handler on a public route asks `security.AuthenticationUnavailableCause(token)` to tell "could not check" from "signed out". Set `UnavailableRetryAfter` on `GlobalConfig` or a firewall's `FirewallOverride` for another delay.

### Security: an authenticator's outage is no failed sign-in and no 500

**What changed.** [`TotpSecondFactorAuthenticator`](../security/totp_second_factor_authenticator.go) returns a failed enrollment lookup, recovery redemption or replay guard marked with [`exception.MarkInfrastructureFailure`](../exception/infrastructure_failure.go). [`AuthenticatorTokenSource`](../security/token_source.go) raises no `security.login.failure` for an authenticator's marked error and returns an authenticator's unavailable token, a token carrying [`securitycontract.AuthenticationUnavailable`](../security/contract/token.go), with no event; the resolution listener answers both as the unavailable token. A sign-in door with an `Authenticator` refuses the same two with the firewall's `503` and `Retry-After` before the session rotation, filing one Error record naming the firewall, the door, the method and the path, none when the cause was already filed; it answered `500` for the error and `401 invalid credentials` with `security.login.failure` for the token. A sign-in `Handler` returning a marked error is refused the same way, and `CompiledFirewall.Login` raises no `security.login.failure` for it. A marked error is read as unavailable unless an http status answers outside what the mark wraps, `NewHttpError(429, {Cause: marked})` keeping its `429`, while a store's own status beneath the mark is answered `503`, at the resolution listener, the door and the handler alike; the failure event follows the answer, raised for a marked error that keeps a `401` or `403`. See [a store that cannot answer](package/SECURITY.md#a-store-that-cannot-answer).

**Symptom.** During a two-factor store or replay guard outage, a sign-in at the door, and a request on a protected route behind an `AuthenticatorTokenSource`, answer `503` with `Retry-After` where they answered `500`, and a public route is served where it answered `500`. A lockout or audit listener on `security.login.failure` no longer counts these. A store whose own error was an http exception no longer has its status and message served at the door. An application authenticator that mapped its outage to a `503` http exception by hand keeps its own status and gets no `Retry-After`.

**Remedy.** Return an authenticator's outage through `exception.MarkInfrastructureFailure` instead of an http exception, so the door answers it with the firewall's delay; keep an http exception for an answer of the application's own, a spent budget's `429`. An application authenticator that answers the unavailable token, `security.NewUnavailableToken(err)`, gets the same answer.

### Security: an authorization decision that could not be made is 503 with `Retry-After`

**What changed.** A voter's [`Vote`](../security/contract/voter.go) answers an error beside its result, and the framework's [`AccessDecisionManager`](../security/access_decision_manager.go) reads it: the error ends the decision whatever the strategy, since a missing vote cannot be counted as an abstention, and is returned marked with [`exception.MarkInfrastructureFailure`](../exception/infrastructure_failure.go) unless it answers a `4xx`, the voter's own refusal, or is already marked, so a `5xx` or an error with no status answers `503`. `DecideAny` grants on any attribute granted and otherwise answers the first error `exception.AnswersUnavailable` reads as unavailable, where it answered `403` for every error. When `DecideAll` answers an error [`exception.AnswersUnavailable`](../exception/infrastructure_failure.go) reads as unavailable, a custom decision manager's included, access control does not consult the access denied handler, files one Error record naming the firewall, the method and the path, none when the cause was already filed, raises `security.authorization.denied` with the new reason `authorization_unavailable` and refuses with `503 service unavailable`, the firewall's `Retry-After` and [`security.ErrAuthorizationUnavailable`](../security/unavailable_token.go) in the chain. An unmarked error is refused as it was. See [a store that cannot answer](package/SECURITY.md#a-store-that-cannot-answer).

**Symptom.** A custom decision manager returning a marked error gets `503` with `Retry-After` where its access denied handler answered, or the error's status, `500` for a plain one. An audit counting `security.authorization.denied` by reason sees `authorization_unavailable`.

**Remedy.** A voter that reads a policy store returns the store's error from `Vote`. Recognise the refusal with `errors.Is(err, security.ErrAuthorizationUnavailable)`. Return an unmarked error, or one answering an http status, from a decision manager where the denial is the intended answer; from a voter, a `4xx` error is the refusal and anything else is the outage.

### Security: `security.login.failure` is raised for an error answered 401 or 403, on every sign-in path

**What changed.** An error an authenticator returns, through [`AuthenticatorTokenSource`](../security/token_source.go) or at a sign-in door, or a sign-in `Handler` returns, raises `security.login.failure` only when it answers `401` or `403`, a refused credential: `exception.Unauthorized("bad password")` returned as an error is a failed sign-in, a malformed request, a refused media type, a spent budget, a plain error, a `5xx` and an outage are none. `AuthenticatorTokenSource` and `CompiledFirewall.Login` raised it for every error not read as unavailable, and the door's authenticator raised it for no error. A failed dispatch of the event keeps the refusal's status and is filed as one Error record, at the door, its `Handler` and `AuthenticatorTokenSource` alike, where the source and `CompiledFirewall.Login` joined the dispatch error into the refusal. A second factor not yet presented, an error carrying `security.ErrSecondFactorRequired`, raises none. A refused credential, an unauthenticated token, raises it as before.

**Symptom.** A lockout or audit listener on `security.login.failure` no longer counts a plain error, a `5xx` or a `4xx` other than `401` and `403` from an authenticator or a sign-in handler, and counts an authenticator's `401` or `403` error at the door.

**Remedy.** Report refused credentials as an unauthenticated token, or as an error answering `401` or `403`; return a fault as a plain error and a malformed request as a `400`.

### Application: a bus whose transports cannot be ordered or closed is refused

**What changed.** Two wiring failures that wrote a warning and left a guarantee unarmed are refused. The application's boot refuses a bus registered under `messagebus.ServiceBus` or `messagebus.ServiceConsumeBus` that the container cannot order before `messagebus.ServiceTransportsCloser`, a ring among them for instance, naming the bus, and a transports closer whose provider fails, naming the closer, the container's error in the chain of each. An event dispatcher carries the required mark and the reserved floor through `eventcontract.EventDispatcher` itself, so one that cannot hold them no longer compiles; see the event section on one dispatcher contract.

**Symptom.** A process whose container wiring of the message bus is broken exits with the refusal at boot, where it served with the warning.

**Remedy.** Register the transports through `messagebus.RegisterTransports`, which registers the closer beside them, and declare no teardown dependency from the closer to a bus.

### Lock: a stopped process keeps its lock until its work returns

**What changed.** After a stop, [`RunExclusive`](../lock/run_exclusive.go) and the [`LeaderGate`](../lock/leader_gate.go) keep renewing the lease while the callback or a blocking `OnElected` winds down, up to `StopRenewalLimit` (30s by default), where the stop ended the renewal at once.

**Symptom.** A deployment that relied on a peer taking the lock over at the stop now waits for the stopping instance's work to return, or for the limit. A callback that ignores its context past the limit makes the run answer `exclusive run lost the lock lease while running`.

A lock store closed by the teardown while the work still winds down now reads as a lost lease (`OnLost`, the run's error), where the stop swallowed it.

**Remedy.** Keep the wind-down short, set `StopRenewalLimit` (also on `ExclusiveCommandConfig`) to the application's teardown timeout and below the orchestrator's grace period, or `bound.UnlimitedDuration()` to renew until the work returns; keep the locker's client open until the work has returned.

### Session: the file storage journals its writes beside the snapshot

**What changed.** [`NewFileStorage`](../session/file_storage.go) over a `Path` appends one synced record per `Save` and `Delete` to `<path>.journal` and rewrites the snapshot only at a compaction: at construction, past `FileStorageConfig.JournalMaxBytes` (8 MiB by default) and at `CloseWithContext`, where every write rewrote the whole snapshot. The snapshot's format is unchanged, so a file the third major wrote opens as it is. `FileStorageConfig` gains `JournalDisabled`, `JournalMaxBytes` and `Logger`. A `File` handle writes the whole snapshot on every write, as before.

**Symptom.** A second file, `<path>.journal`, appears beside the snapshot, and between two compactions the snapshot alone does not hold the latest writes. A build that does not read the journal (the third major, or an earlier build of this one) opened on the same path after a crash loses every write since the last compaction; once it has rewritten the snapshot, the journal is discarded, with a warning, when the journaling build opens the path again.

**Remedy.** Nothing for a deployment that stops cleanly, since the close compacts. Before a rollback to a build without the journal, stop the process cleanly, or start once with `JournalDisabled: true`, which replays and compacts a journal left at the path and removes it; a deployment that needs the snapshot alone at every instant keeps `JournalDisabled: true`. A backup copies both files, or copies the snapshot after a clean stop.

### Cache: a counter carries its ttl

**What changed.** [`cachecontract.Cache`](../cache/contract/cache.go) and `Backend` take `Increment(runtimeInstance, key, delta, options)` and `Decrement(runtimeInstance, key, delta, options)`, the options a [`CounterOptions`](../cache/contract/counter.go): `Ttl`, read as a write's ttl (at least 1s, `bound.UnlimitedDuration()` for none, absent refused), and `RefreshTtl`, false for a fixed window written when the call creates the key or finds it without an expiry, true for a sliding window rewritten on every call; an unlimited sliding call removes an expiry the key carries. The in-memory and the redis backends implement both, the redis one in one script.

**Symptom.** A call written for the third major does not compile; a `Backend` or `Cache` of the application's own no longer satisfies the interface; a call with an empty `CounterOptions` answers the error `the cache ttl is required`.

**Remedy.** `cache.Increment(ctx, key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})` keeps the third major's behaviour: a counter created without an expiry, and one the key already carries kept, exactly as `INCRBY` did; `RefreshTtl: true` with an unlimited ttl differs, since it removes that expiry; a rate window or a daily count passes its span, `RefreshTtl: true` for a span that restarts on every call. An implementation of the interfaces writes the expiry in the same atomic operation as the count.

### Cache: a read names its type

**What changed.** [`cachecontract.Serializer`](../cache/contract/serializer.go) decodes into a target: `Deserialize(payload []byte, target any) error`, the signature of [`serializercontract.Serializer`](../serializer/contract/serializer.go). [`cachecontract.Cache`](../cache/contract/cache.go) reads into a target too: `Get(ctx, key, target any) (bool, error)` and `Many(ctx, keys, target any) error`, the target of `Many` a pointer to a `map[string]T`, replaced whole; `Get` overwrites its target on a hit and leaves it untouched on a miss, a failure or a payload that does not decode, which answers `cache.DeserializationError`. `Cache` gains `RoundTrip(value, target any) error`, what a value reads back as once stored, and `cachecontract.StoredValueNormalizer` with `Manager.NormalizeStoredValue` is removed. The package doors are generic: [`cache.Get[T]`](../cache/typed.go), `cache.Many[T]` and [`cache.Remember[T]`](../cache/remember.go), `T` inferred from the callback; the callers of one key coalesce per type, and two types under one key lead two flights, each overwriting the other's entry.

**Symptom.** The third major's `cacheInstance.Get(key)` and a `Serializer` or `Cache` of the application's own no longer compile; a callback whose result is `any` compiles as `Remember[any]`, and once it names its type the `value.(T)` after the call no longer compiles, `value` being no interface; `cachecontract.StoredValueNormalizer` is undefined; at run time, a key two call sites read as two types is recomputed on every alternation when the serializer refuses one type for the other, and under json, which refuses a mismatch of kind only, a struct written as another struct reads as a hit with the fields they do not share at zero.

**Remedy.** Read through `value, exists, err := cache.Get[T](boundCache, key)`, and declare the callback's result type of `Remember` and drop the assertion that follows it; `cache.Get[any]` and `Remember[any]` keep the generic answer. A serializer implements `Deserialize(payload, target)` by decoding into the target, `json.Unmarshal(payload, target)` for json. A decorator that embeds `cachecontract.Cache` needs nothing; a full implementation adds `RoundTrip` and fills its read targets. Give each type its own key.

### Http: `GeneratePath` takes a config, `GenerateUrl` is absolute

**What changed.** [`UrlGenerator.GeneratePath`](../http/url_generator.go) takes the route name and a [`PathConfig`](../http/contract/url_generator.go)`{Parameters, Query, Fragment}`, where it took a `map[string]string` of parameters; the query is a `url.Values`, so a repeated key is expressible, an empty key skipped and the keys written sorted, and the fragment is written after it. [`GenerateUrl`](../http/url_generator.go) is the absolute door: the same reference behind the public base `MELODY_HTTP_PUBLIC_URL`, whose default is the new `ApplicationConfig.HttpPublicUrl`, a path the base carries joined before the route's with one `/`; a request's `Host` is never read. `NewUrlGenerator(registry, UrlGeneratorConfig{PublicUrl})` takes the base.

**Symptom.** A `GenerateUrl` call that compiled answers an error naming `MELODY_HTTP_PUBLIC_URL` when no base is set, where it answered the path and its query. A `map[string]string` handed to `GeneratePath` or `GenerateUrl`, and `NewUrlGenerator(registry)`, no longer compile. A base that is not an `http` or `https` url with a host name, or that carries user info, a query, a fragment or an empty, `.` or `..` path segment, refuses the boot naming the key or the field. The path of either door is written by `url.URL`, so a static route segment holding a byte a url escapes is answered percent-encoded, `/über/:id` reading `/%C3%BCber/…`, where it was answered raw.

**Remedy.** `GeneratePath(name, httpcontract.PathConfig{Parameters: parameters})`; where a relative path with a query is wanted, `GeneratePath(name, httpcontract.PathConfig{Parameters: parameters, Query: url.Values{"page": {"2"}}})` in place of `GenerateUrl`. Set `MELODY_HTTP_PUBLIC_URL` or `ApplicationConfig.HttpPublicUrl` to the url the application is served under before calling `GenerateUrl`. A comparison against a generated path whose static segment holds such a byte expects the percent-encoded spelling.

### Logs: the session reference is `sessionReference`

**What changed.** The log and error context key that carries the digest of a session id, written by the session manager, the request's session reader and the session persistence records, is `sessionReference`, where it was `sessionRef`; the value is unchanged and no record carries both keys. The http start-up record reads "starting http server on `…` with environment `…`".

**Symptom.** Nothing fails: a dashboard, an alert or a log query keyed on `sessionRef`, or matching the start-up text "with env", stops matching.

**Remedy.** Re-key the query on `sessionReference` and match "with environment".

### Http: the type parameter is `RequestBody`

**What changed.** The type parameter of [`JsonHandler`](../http/typed_handler.go) is `RequestBody`; `NewRequest` names its route parameter map `routeParameters`. The example's frontend helper `melody-routes.ts` exports `RouteParameters` and reads `data-route-parameters`.

**Symptom.** None for Go code: a type parameter's name is not part of a call. A frontend that vendored the helper and imports `RouteParams`, or writes `data-route-params`, no longer type checks or is no longer read.

**Remedy.** Import `RouteParameters` and write `data-route-parameters` when the helper is vendored again.

### Container: service names follow one grammar

**What changed.** Every service name follows `service.<module>.<snake_case_name>` (see [CONTAINER](package/CONTAINER.md#service-names)); the names that split one name's words by dots, or carried no `service.` prefix, are renamed, the Go constants keeping their names:

| Constant | Old name | New name |
|---|---|---|
| `http.ServiceRouteRegistry` | `service.http.route.registry` | `service.http.route_registry` |
| `http.ServiceUrlGenerator` | `service.http.url.generator` | `service.http.url_generator` |
| `http.ServiceRequestContext` | `service.http.request.context` | `service.http.request_context` |
| `migrate.DefaultManagerRegistryServiceId` | `service.database.manager.registry` | `service.bunorm.manager_registry` |
| `otlp.ServiceTracerProvider` | `opentelemetry.otlp.tracer_provider` | `service.opentelemetry.otlp.tracer_provider` |

The six `service.<module>` names of a module's principal service, `service.cache`, `service.clock`, `service.config`, `service.logger`, `service.serializer` and `service.validator`, are unchanged. No old name is kept beside the new one.

**Symptom.** Code that resolves through the constants is unaffected. A name typed as a string, a `debug:container` filter or a wiring declaration naming an old name finds no service, and an override of an old name is refused with `service not registered in container`; a bunorm registry registered under `service.database.manager.registry` is no longer the one the migrate commands find by default.

**Remedy.** Use the constants, or the new names of the table; register the bunorm registry under `migrate.DefaultManagerRegistryServiceId` or name yours through `ManagerRegistryServiceId`. The example's own names follow the grammar too, for an application that copied them:

| Old name | New name |
|---|---|
| `service-example-archive-database` | `service.example.archive_database` |
| `service-example-category-service` | `service.example.category_service` |
| `service-example-currency-service` | `service.example.currency_service` |
| `service-example-database` | `service.example.database` |
| `service-example-database-registry` | `service.example.database_registry` |
| `service-example-product-service` | `service.example.product_service` |
| `service-example-rate-refresh-service` | `service.example.rate_refresh_service` |
| `service-example-two-factor-store` | `service.example.two_factor_store` |
| `service-example-user-service` | `service.example.user_service` |
| `service.example.archive.locker` | `service.example.archive_locker` |
| `service.example.archive.storage` | `service.example.archive_storage` |
| `service.example.catalog.journal.repository` | `service.example.catalog_journal_repository` |
| `service.example.catalog.journal.service` | `service.example.catalog_journal_service` |
| `service.example.catalog.notification.hub` | `service.example.catalog_notification_hub` |
| `service.example.catalog.reading.repository` | `service.example.catalog_reading_repository` |
| `service.example.catalog.storage` | `service.example.catalog_storage` |
| `service.example.category.repository` | `service.example.category_repository` |
| `service.example.currency.repository` | `service.example.currency_repository` |
| `service.example.internal_auth.signer` | `service.example.internal_auth_signer` |
| `service.example.product.repository` | `service.example.product_repository` |
| `service.example.rates.http.client` | `service.example.rates_http_client` |
| `service.example.report.export.http.client` | `service.example.report_export_http_client` |
| `service.example.reporting.catalog.journal.source` | `service.example.reporting_catalog_journal_source` |
| `service.example.security.token_namespace` | `service.example.security_token_namespace` |
| `service.example.seeder.category` | `service.example.seeder_category` |
| `service.example.seeder.currency` | `service.example.seeder_currency` |
| `service.example.seeder.product` | `service.example.seeder_product` |
| `service.example.seeder.user` | `service.example.seeder_user` |
| `service.example.user.repository` | `service.example.user_repository` |
| `service.example.user.session.repository` | `service.example.user_session_repository` |

### Amqp: the transport's round-trip bound is `CallTimeout`

**What changed.** The amqp [`TransportConfig.PublishTimeout`](../../integrations/amqp/v4/transport.go) is `CallTimeout`, the name the backplane uses for the same bound and the rueidis cache's `BackendConfig` uses for its round trips; its refusal names `TransportConfig.CallTimeout`, the log and error context key `publishTimeout` is `callTimeout` and the publish refusals read "within the call timeout" where they read "within the publish timeout". Cron's `ErrCommandTimeout`, a scheduled command run past its timeout, keeps its name.

**Symptom.** `amqp.TransportConfig{PublishTimeout: …}` no longer compiles; a log query keyed on `publishTimeout`, or an alert matching "within the publish timeout", stops matching.

**Remedy.** Rename the field to `CallTimeout`, `bound.Of(d)` as before, re-key the query on `callTimeout` and match "within the call timeout".

### Static files: `MELODY_STATIC_CACHE_DISABLED` replaces `MELODY_STATIC_ENABLE_CACHE`

**What changed.** The built-in file server's cache switch is `MELODY_STATIC_CACHE_DISABLED`, parameter `kernel.static.cache_disabled`, `false` by default, read through [`HttpConfiguration.StaticCacheDisabled()`](../config/contract/http.go), which replaces `StaticEnableCache()`; `StaticEnableCacheKey` and `KernelStaticEnableCache` are `StaticCacheDisabledKey` and `KernelStaticCacheDisabled`. `MELODY_STATIC_ENABLE_CACHE` is retired. The disabled cache wins over a valid max age set beside it, a `.env` artifact's line, blank or `3600` included: the `http.static.cacheMaxAgeIgnored` boot warning an http process writes names each value where it came from, the key of a `.env` line under `environmentKeys` and the parameter of an override under `parameterNames`, where the max age was dropped without a word.

**Symptom.** A boot whose `.env` still sets `MELODY_STATIC_ENABLE_CACHE` fails with `the environment key MELODY_STATIC_ENABLE_CACHE is retired: rename it to MELODY_STATIC_CACHE_DISABLED, and the value inverts: MELODY_STATIC_ENABLE_CACHE=false is MELODY_STATIC_CACHE_DISABLED=true`. A boot that sets `MELODY_STATIC_CACHE_DISABLED=true` beside `MELODY_STATIC_CACHE_MAX_AGE` writes the `http.static.cacheMaxAgeIgnored` boot warning. A call of `StaticEnableCache()` no longer compiles.

**Remedy.** **Invert the value when renaming the key**: `MELODY_STATIC_ENABLE_CACHE=false` is `MELODY_STATIC_CACHE_DISABLED=true`, and `MELODY_STATIC_ENABLE_CACHE=true` is the default, the line dropped or `MELODY_STATIC_CACHE_DISABLED=false`. Drop the max age from a file that disables the cache. Read `StaticCacheDisabled()` where `false == StaticEnableCache()` was read.

### Stop grace: one name, `ShutdownTimeout`, and `ApplicationConfig.HttpServer.ShutdownTimeout`

**What changed.** The wait a stopping process gives the work it admitted has one name, `ShutdownTimeout`, which the consume command's [`ConsumeCommandConfig.ShutdownTimeout`](../messagebus/consume_command.go) carries in place of `WithShutdownGrace`: the cron [`RunnerConfig.GracefulTimeout`](../../integrations/cron/v4/runner_config.go) and [`EntryConfig.GracefulTimeout`](../../integrations/cron/v4/configuration.go) are `ShutdownTimeout`, their refusals naming `RunnerConfig.ShutdownTimeout` and `EntryConfig.ShutdownTimeout`, and the cron log key `gracefulTimeout` and the context key `unwindGrace` of the abandon errors are `shutdownTimeout`; the entry's field still bounds the unwind after its own `Timeout` too. The http wait gains its field, [`ApplicationConfig.HttpServer.ShutdownTimeout`](../application/application_new.go), the default of `MELODY_HTTP_SHUTDOWN_TIMEOUT`, which wins when a deployment sets it, 5s without either; `HttpConfiguration.ShutdownTimeout()` answers the key as the deployment set it, a `bound.Duration` absent when unset, and a refused value names `ApplicationConfig.HttpServer.ShutdownTimeout`.

**Symptom.** `RunnerConfig{GracefulTimeout: …}` and `EntryConfig{GracefulTimeout: …}` no longer compile; `Http().ShutdownTimeout().Value` no longer compiles; a log query keyed on the cron `gracefulTimeout` or `unwindGrace` stops matching.

**Remedy.** Rename the fields to `ShutdownTimeout`; set the http wait in code with `ApplicationConfig{HttpServer: application.HttpServerConfig{ShutdownTimeout: bound.Of(d)}}`, the key keeping the last word; read `Http().ShutdownTimeout()` as an optional limit, or keep the key's value out of the code and let the application resolve it.

### Cors: `AllowOrigin` receives the request

**What changed.** [`cors.Config`](../http/cors/service.go)'s origin function is `AllowOrigin func(request httpcontract.Request, origin string) bool`, where it was `AllowOriginFunc func(origin string) bool`, and `Service.OriginAllowed` takes the request first: `OriginAllowed(request, origin)`. The request's `RuntimeInstance()` reaches the context and the container, so a per-tenant decision reads its store through it; a lookup the function cannot make answers false, refusing the origin. A nil request handed to `OriginAllowed` while a function is configured panics naming `cors.Service.OriginAllowed`.

**Symptom.** A `cors.Config{AllowOriginFunc: …}` literal and a call `service.OriginAllowed(origin)` no longer compile.

**Remedy.** Write `AllowOrigin: func(request httpcontract.Request, origin string) bool { … }`, ignoring the request where the decision does not read it, and pass the request being served to `OriginAllowed`.

### Container: a service is resolved through `…FromResolver` only

**What changed.** Every service accessor named `…FromContainer`, the framework's and the integrations', is removed beside its `…FromResolver` form: a [`Container`](../container/contract/container.go) is a `Resolver`, so `cache.CacheMustFromResolver(ctx, container)` takes the container exactly as it takes a scope. The seven with no resolver form, `validation.ValidatorMustFromContainer`, `validation.ValidatorFromContainer`, `config.ConfigFromContainer`, `session.SessionMustFromContainer`, `http.RouteRegistryMustFromContainer`, `http.UrlGeneratorMustFromContainer` and `http.RouterMustFromContainer`, are renamed `…FromResolver` and take a `containercontract.Resolver`; `ValidatorFromResolver` and `FirewallManagerFromResolver` still answer nil for a service that is not registered. The constructors `messagebus.NewConsumeCommandFromContainer` and `openapi.NewGenerateCommandFromContainer` are unchanged.

**Symptom.** A call of `cache.CacheMustFromContainer(…)`, `logging.LoggerMustFromContainer(…)` or any other `…FromContainer` accessor no longer compiles.

**Remedy.** Replace `FromContainer` with `FromResolver` in the name and keep the argument: `cache.CacheMustFromResolver(ctx, resolver)` in a provider. With a runtime in hand, where the accessor has a `FromRuntime` form, resolve through the runtime instead: `clock.ClockMustFromRuntime(runtimeInstance)`, `cache.FromRuntime(runtimeInstance)` for the bound cache, or `runtime.FromRuntime[T](runtimeInstance, name)` for a service of your own.

### Security: a voter and the decision manager receive the runtime

**What changed.** [`Voter.Vote`](../security/contract/voter.go) is `Vote(runtimeInstance, token, attribute, subject) (VoteResult, error)` and [`AccessDecisionManager`](../security/contract/access_decision_manager.go)'s `DecideAll` and `DecideAny` take the runtime first; the access control listener hands its own runtime. One door answers the vote and its error: `securitycontract.FallibleVoter` and `VoteWithError` are removed, and the framework's manager reads every voter's error by the rules it applied to a fallible one. `Voter.Supports` is unchanged.

**Symptom.** A `Voter` of the application's own no longer satisfies the interface; `securitycontract.FallibleVoter` is undefined; `manager.DecideAll(token, attributes, subject)` reports not enough arguments; a call handed a nil runtime, a typed nil one or one carrying no context is refused before any voter with `the runtime is nil or carries no context`.

**Remedy.** Add the runtime as the first parameter and `nil` as the second result; a voter that implemented `VoteWithError` renames it `Vote` and deletes the old one; a direct caller hands a live runtime, the request's; a handler calls `DecideAll(runtimeInstance, token, attributes, subject)`; read a repository through `runtimeInstance.Scope()` under `runtimeInstance.Context()` instead of asserting the subject to a request.

### Security: the decision manager's strategy is named, its outcomes are a config

**What changed.** [`NewAccessDecisionManager`](../security/access_decision_manager.go) is `NewAccessDecisionManager(strategy, voters, config AccessDecisionManagerConfig)`. [`DecisionStrategyUnspecified`](../security/contract/decision.go) is the zero value of `DecisionStrategy` and is refused at construction, so the three strategies are 1, 2 and 3. `AccessDecisionManagerConfig{AllowIfAllAbstain, AllowIfTied}` carries the two outcomes no vote decides; its zero value refuses both, as the manager did. `AccessDecisionManager.WithVoters(voters)` answers a copy under the same strategy and config, and the compiler adds a role hierarchy through it.

**Symptom.** `NewAccessDecisionManager(strategy, voters)` reports not enough arguments; the boot panics with `access decision strategy is missing` for a strategy written as `0` or left to a zero-valued variable; a strategy number kept in the application's own configuration reads one lower than its meaning; a manager built as a struct literal answers `access decision strategy is missing` from `DecideAll` and `DecideAny`; a vote result outside the three constants refuses the decision with `security voter answered an invalid vote result`; an empty attribute is refused before any voter.

**Remedy.** Pass `security.AccessDecisionManagerConfig{}` for the outcomes as they were; name the strategy constant rather than its number. A strategy persisted as a number is re-written before the upgrade, one higher: only a stored `0` is refused, while a stored `1` boots as `DecisionStrategyAffirmative` and a stored `2` as `DecisionStrategyConsensus`, each more permissive than the strategy the number named. Build the manager through `NewAccessDecisionManager`; a voter answers one of `VoteAbstain`, `VoteDenied` and `VoteGranted`.

### Security: a token source declares whether it is stateless

**What changed.** [`TokenSource`](../security/contract/token_source.go) gains `Stateless() bool`: true when the source reads only a credential the caller presents on every request (a bearer token, a signed envelope, an api key) and nothing a browser attaches by itself. The bearer and HMAC sources answer true; [`NewResolverTokenSource`](../security/token_source.go) and `NewAuthenticatorTokenSource` take `ResolverTokenSourceConfig{Stateless}` and `AuthenticatorTokenSourceConfig{Stateless}`, false by default; the impersonation source answers its inner one's. The firewall's cross-site request forgery check reads the answer: a firewall whose source is stateless checks none of its routes, its doors still refusing a stated other origin.

**Symptom.** A `TokenSource` of the application's own misses `Stateless`; `NewResolverTokenSource(resolver)` and `NewAuthenticatorTokenSource(manager)` report not enough arguments.

**Remedy.** Answer `false` unless the source reads only what the caller writes on every request; pass the zero config, or `Stateless: true` for a source of that kind.

### Security: an authenticator, a rule and a token resolver receive the runtime

**What changed.** [`Authenticator.Authenticate`](../security/contract/authenticator.go) is `Authenticate(runtimeInstance, request) (Token, error)`, [`Rule.Check`](../security/contract/rule.go) is `Check(runtimeInstance, request) error` and [`TokenResolver`](../security/contract/token.go) is `func(runtimeInstance, request) (Token, error)`; `AuthenticatorManager.Authenticate` and `Firewall.Check` take the runtime first and refuse a nil runtime, a typed nil one and one carrying no context before their first authenticator or rule, the manager answering no authenticator used. The framework hands the runtime of the request it serves, and the TOTP second factor reads its stores through it. A resolver's error is answered as a token source's: refused with its status, `500` for a plain one, and read as unavailable when it carries `exception.MarkInfrastructureFailure`; a typed nil error is no error. `Supports`, `Applies` and `Matches` are unchanged.

**Symptom.** An `Authenticator`, a `Rule` or a `TokenResolver` of the application's own no longer compiles; `request.RuntimeInstance()` inside one keeps compiling and keeps working.

**Remedy.** Add the runtime as the first parameter and read stores through it; a resolver returns `token, nil`, or its store's error (marked with `exception.MarkInfrastructureFailure` for an outage) where it built `security.NewUnavailableToken(err)`, which also stays valid.

### Container: the container closes through `CloseWithContext` only

**What changed.** [`containercontract.Container`](../container/contract/container.go) no longer declares `Close() error`, which was `CloseWithContext(context.Background())`; the container's one close door is `CloseWithContext(closeContext)`, a teardown that runs once and hands what is left of the caller's deadline to every service it closes.

**Symptom.** `serviceContainer.Close()` no longer compiles; a `Container` of the application's own still compiles with its `Close` left on it, but nothing calls it.

**Remedy.** Call `serviceContainer.CloseWithContext(ctx)`, `context.Background()` for no deadline.

The name stays `CloseWithContext` on purpose: a `Close(ctx)` could not also satisfy `io.Closer`, and the suffix says at the call site that the close is bounded (see [the policy](#versioning-policy-for-breaking-changes)).

### Config: an environment source loads under a context

**What changed.** [`EnvironmentSource.Load`](../config/contract/environment_source.go) is `Load(ctx context.Context) (map[string]string, error)` and [`config.NewEnvironment`](../config/environment.go) is `NewEnvironment(ctx, source)`, a nil context refused naming the door; `NewApplication` hands the context it was given. The framework's file source judges the context once, before it reads, since a file read waits on nothing; a source that reads a remote store bounds its read by it.

**Symptom.** An `EnvironmentSource` of the application's own no longer satisfies the interface; `config.NewEnvironment(source)` reports not enough arguments.

**Remedy.** Add the context as the parameter of `Load` and end a remote read on it; pass a context to `NewEnvironment`, `context.Background()` where none is at hand.

### Event: one event constructor, stamped by the caller

**What changed.** [`event.NewEvent`](../event/event.go) is `NewEvent(name, payload, timestamp time.Time)`, where it took a clock, and `NewEventWithTimestamp`, its twin, is removed. `DispatchName` stamps the event with its dispatcher's clock as before.

**Symptom.** `event.NewEvent(name, payload, clock)` and `event.NewEventWithTimestamp(…)` no longer compile.

**Remedy.** Pass `clock.Now()` where a clock was passed, and rename `NewEventWithTimestamp` to `NewEvent`.

### Logging: a logger answers `Enabled` and `CauseChainDepth`

**What changed.** [`loggingcontract.Logger`](../logging/contract/logger.go) carries `Enabled(level Level) bool` and `CauseChainDepth() int`. The optional `loggingcontract.LevelReporter` and `loggingcontract.CauseChainDepthReporter` are removed, with `logging.LevelEnabled` and `logging.CauseChainDepth`: a caller asks the logger itself, and a decorator that forgets to forward can no longer turn every level on or reset the depth to 8 unseen.

**Symptom.** `*L does not implement loggingcontract.Logger (missing method CauseChainDepth)` or `(missing method Enabled)`; `undefined: loggingcontract.LevelReporter`, `loggingcontract.CauseChainDepthReporter`, `logging.LevelEnabled`, `logging.CauseChainDepth`.

**Remedy.** Give a logger of the application's both methods: `Enabled` answering its threshold, `true` for one with none, and `CauseChainDepth` the links its error records carry, 8 for the framework's default; a decorator forwards both to the logger it wraps, or embeds `loggingcontract.Logger`. Write `logger.Enabled(level)` for `logging.LevelEnabled(logger, level)` and `logger.CauseChainDepth()` for `logging.CauseChainDepth(logger)`.

### Event: one dispatcher contract, and a registration declares its listener

**What changed.** [`eventcontract.EventDispatcher`](../event/contract/event_dispatcher.go) carries `InstallFloorListener` and `RegisteredEvents`; `RequiredListenerRegistrar`, `PriorityFloorRegistrar` and `EventDispatcherInspector` are removed, with `MarkListenerRequired` and `MarkListenerMaySkipRequiredListeners`. `AddListener(eventName, listener, config)` and `InstallFloorListener(eventName, listener, config)` take an [`eventcontract.ListenerConfig`](../event/contract/event_dispatcher.go) of `Priority`, `Name`, `Required`, `MaySkipRequiredListeners` and `Tags`, so a required listener is registered in one call; `SubscribedEvent` answers `Config() ListenerConfig` in place of `Priority() int`, and `NewSubscribedEvent(listener, config)` takes one. `NewEventDispatcher(EventDispatcherConfig{Clock})` takes a config, the system clock when `Clock` is nil, a typed nil refused. `NewRequiredListenerSkippedErrorWithCause` is removed. The access control listener refusing a dispatcher without the required mark, the http kernel refusing one without the floor, the adapter's two refusals and `debug:events` warning about a dispatcher it cannot inspect are gone: the compiler says it.

**Symptom.** `cannot use 10 (untyped int constant) as eventcontract.ListenerConfig value in argument to dispatcher.AddListener`; `dispatcher.MarkListenerRequired undefined`; `undefined: eventcontract.RequiredListenerRegistrar`, `eventcontract.PriorityFloorRegistrar`, `eventcontract.EventDispatcherInspector`, `event.NewRequiredListenerSkippedErrorWithCause`; `*S does not implement eventcontract.SubscribedEvent (missing method Config)`; a dispatcher of the application's own `does not implement eventcontract.EventDispatcher (missing method InstallFloorListener)`.

**Remedy.** Write `AddListener(name, listener, eventcontract.ListenerConfig{Priority: 10})`, `ListenerConfig{}` for priority 0; fold `MarkListenerRequired(registration)` into `ListenerConfig{Failure: ListenerFailureConfig{Required: true}}` and `MarkListenerMaySkipRequiredListeners(registration)` into `ListenerConfig{Failure: ListenerFailureConfig{MaySkipRequiredListeners: true}}`; `NewSubscribedEvent(listener, eventcontract.ListenerConfig{Priority: 10})`; `event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clockInstance})`, or `EventDispatcherConfig{}` for the system clock. A dispatcher of the application's own implements the two methods, forwarding to the one it wraps.

### Event: the first failure still ends the dispatch; tags run after it

**Not changed.** As in the third major, the first listener that fails — a returned error or a recovered panic — ends the dispatch: no listener behind it runs, and [`Dispatch`](../event/event_dispatcher.go) answers that failure itself.

**What changed.** A listener carries tags, `ListenerConfig.Tags`, and the owner of an event declares once, through the new `EventDispatcher.DeclareEvent(eventName, eventcontract.EventConfig{TagsToRunAfterFailure})`, which tags still run after a failure: every listener behind the failed one carrying a declared tag runs, reads the failure through the new `Event.PriorFailure()` and has its own failure joined to the first, in run order. A deliberate stop runs none of them, and an event nobody declared behaves as in the third major. The application declares the framework's twelve events itself, at `NewApplication`, from the new `ApplicationConfig.Events`, so `DeclareEvent` on a `kernel.*`, `security.*` or `http.request.profile` name is refused with "the event is already declared"; the access log and the cors response decoration carry the framework's tags `melody.access_log` and `melody.cors` and still run after a failed listener, cors adding headers only. A listener running after a failure writes the response of `kernel.request`, `kernel.controller`, `kernel.response` and `kernel.exception`, or refuses on `kernel.request`, only through the new `ListenerConfig.Failure.ReplacesResponseAfterFailure`, the kernel warning either way and failing closed when a required listener did not run. `RegisteredEvent` gains `Declared` and `Config`, `RegisteredListener` carries `Config` and `RunsAfterFailure`.

As in the third major, a failure ahead of a `Required` listener that did not run is refused with `RequiredListenerSkippedError`, the failure as its cause, whatever the failed listener declared, `MaySkipRequiredListeners` not read for a listener that failed; a required listener a tag ran after the failure is not refused.

**Behavioural change.** On `kernel.request` and `kernel.controller` the http kernel answers its fail-closed 500 for any failed dispatch, dropping a response a listener set beside the failure, where the third major served such a response when no required listener was skipped; a refusal through `KernelRequestEvent.Refuse` still stands with its rendered status, unless a required listener was skipped or a second decision was attempted, which answer the 500. On `kernel.exception` a failed listener ends the dispatch before the built-in listener, and the kernel renders the error itself, with the same body and status; an application answer is served only when no `kernel.exception` listener failed.

**Symptom.** `*E does not implement eventcontract.Event (missing method PriorFailure)` for an event of the application's own; `*D does not implement eventcontract.EventDispatcher (missing method DeclareEvent)` for a dispatcher of its own. A `kernel.request` listener that set a response and failed gets the 500 page.

**Remedy.** An event of the application's own answers `func (…) PriorFailure() error { return nil }`; a dispatcher of its own forwards `DeclareEvent` to the one it wraps. Read the failure through `errors.Is` / `errors.As`, which reach it through the refusal's cause; a listener that must run after a failure ahead of it carries a tag its event declares; on a framework event the tag goes in `ApplicationConfig.Events["<name>"].TagsToRunAfterFailure`, not through `DeclareEvent`; a listener that must replace the response after a failure declares `Failure.ReplacesResponseAfterFailure`. A listener that answers a request returns nil; a listener that only observes journals its own failure and returns nil, so the listeners behind it run.

### Event: a listener's failure fields move under `ListenerConfig.Failure`; reports carry the config

**What changed.** `ListenerConfig` is `Priority`, `Name`, `Tags` and `Failure`, an `eventcontract.ListenerFailureConfig` of `Required`, `MaySkipRequiredListeners` and `ReplacesResponseAfterFailure`. `RegisteredListener` carries `Config`, the `ListenerConfig` registered, beside `Source`, `Owner`, `ListenerId`, the resolved `ListenerName` and `RunsAfterFailure`; `RegisteredEvent` carries `Config`, the `EventConfig` declared. `debug:events --format=json --verbose` moves `priority` and `tags` under `config`, `required`, `maySkipRequiredListeners` and `replacesResponseAfterFailure` under `config.failure`, and a declaration's `tagsToRunAfterFailure` under `config`.

**Symptom.** `unknown field Required in struct literal of type eventcontract.ListenerConfig`; `listener.Priority undefined (type eventcontract.RegisteredListener has no field or method Priority)`.

**Remedy.** Write `eventcontract.ListenerConfig{Priority: 10, Failure: eventcontract.ListenerFailureConfig{Required: true}}`; read `listener.Config.Priority`, `listener.Config.Failure.Required` and `registeredEvent.Config.TagsToRunAfterFailure`; a consumer of the json reads the keys at their new place.

### Http: the first responder ends the `kernel.request` and `kernel.controller` dispatch

**What changed.** An answer or a refusal on `kernel.request`, through `KernelRequestEvent.SetResponse` or `Refuse`, and an answer on `kernel.controller` end the dispatch, as an answer on `kernel.exception` already did: no listener behind it runs. `kernel.response` is unchanged, its chain replacing the response in turn. A decision ahead of a required listener makes the dispatch answer `RequiredListenerSkippedError`, so the kernel answers its fail-closed 500, unless the deciding listener declares `Failure.MaySkipRequiredListeners`. The framework's deciders on `kernel.request` declare it: the rate limit request listener, the cors preflight listener, the firewall listener and its doors, and the access control listener, which stays required. The cors response listener writes the preflight headers on a preflight that a listener ahead of the preflight listener answered or refused, its status and body untouched.

**Symptom.** A `kernel.request` listener at a low priority no longer sees a request a listener above it answered or refused. A listener of the application's own that answers ahead of a required listener gets the 500 page, naming the stopping listener in development.

**Remedy.** A listener that must see every request sits at a priority above the deciders, or on `kernel.response` or `kernel.terminate`; a listener that answers ahead of a required listener declares `Failure.MaySkipRequiredListeners`, as does the registration of a listener that calls `RateLimitGate.Apply`.

### Event: names are constants; the `melody.` namespace is the framework's

**What changed.** `http.EventHttpRequestProfile` is `httpcontract.EventHttpRequestProfile`, and `kernelcontract.ListenerTagAccessLog` and `ListenerTagCors` are `ListenerTagKernelAccessLog` and `ListenerTagKernelCors`, their values unchanged. A tag in the `melody.` namespace the framework does not define is refused at `AddListener`, `InstallFloorListener`, `AddSubscriber` and `DeclareEvent`, and through `ApplicationConfig.Events`, which declares through `DeclareEvent` under its rules: a framework tag listed again on the event that carries it is refused as listed twice, and a framework tag on another framework event is declared.

**Symptom.** `undefined: http.EventHttpRequestProfile`; `undefined: kernelcontract.ListenerTagCors`; `ListenerConfig.Tags carries a tag in the melody. namespace the framework does not define`.

**Remedy.** Use the new names; a tag of the application's own takes a namespace of its own, and a listener that must run beside a framework listener binds the framework's constant.

**For whoever followed the development line.** Between 2026-11-18 and this change the fourth major's branch ran every listener, joining their failures, and carried `ListenerConfig.FailureEndsDispatch` for the listeners whose failure ended the dispatch. Neither shipped: remove the flag, and where a listener relied on running after a failed one, tag it and declare the tag on its event.

### Httpclient: a typed credential beside the request's own `Authorization` header is refused

**What changed.** [`RequestOptions`](../httpclient/contract/request_option.go) carrying `BearerToken` or `BasicAuth` beside an `Authorization` entry of its own `Headers` is refused with "the request names two credentials", naming `RequestOptions.Headers`, as the pair of credentials is, where the typed credential won in silence. A typed credential still wins over the client's own `Authorization` header.

**Symptom.** A request that set both now fails with that refusal before anything is sent.

**Remedy.** Keep one: the typed credential or the header.

### Translation: the manager takes its catalogs in its config and has no default locale of its own

**What changed.** [`translation.NewManager(ManagerConfig{Catalogs, FallbackLocales})`](../translation/manager.go) takes the catalogs in the config; `ManagerConfig.DefaultLocale` is removed: a lookup asks the locale, its base, then `FallbackLocales` in order, and nothing after the last, so the application ends the list on its default locale, `kernel.default_locale`, and declares that fact once.

**Symptom.** `too many arguments in call to translation.NewManager`; `unknown field DefaultLocale in struct literal of type translation.ManagerConfig`.

**Remedy.** `translation.NewManager(translation.ManagerConfig{Catalogs: catalogs, FallbackLocales: []string{…, configuration.Http().DefaultLocale()}})`.

### Wiring: the generation target is a `GenerateConfig`, and a constructor is kept out by its directive

**What changed.** [`NewGenerateCommand(bindSet, config)`](../wiring/generate_command.go) takes a [`GenerateConfig{PackageName, FunctionName, ScopedFunctionName, BuildTags}`](../wiring/generator.go), the target declared once in Go beside the bind set, and `GenerateRequest` carries it as `Config`, so the command and a bootstrap program write the same file; the `--package`, `--function`, `--scoped-function` and `--tags` flags are removed, `--out`, `--strict`, `--report-vendor` and `--report-excluded` kept. `PackageBinding.Exclude`, `Excludes()`, `ScanResult.UnusedExcludes`, `GenerateReport.UnusedExcludes` and the unused-exclude report are removed: `//melody:ignore <reason>` on the constructor keeps it out.

**Symptom.** `flag provided but not defined: -package`; `not enough arguments in call to wiring.NewGenerateCommand`; `unknown field PackageName in struct literal of type wiring.GenerateRequest`; `packageBinding.Exclude undefined`.

**Remedy.** Declare `wiring.GenerateConfig{PackageName: "generated", FunctionName: "RegisterGeneratedServices"}` beside the bind set and hand it to `NewGenerateCommand` and to `GenerateRequest{Config: …}`; put `//melody:ignore <reason>` on a constructor an exclude pattern kept out.

### Http, application: a shutdown hook takes the shutdown's context, a middleware factory answers an error, the json refusal is one struct

**What changed.** [`Application.OnHttpShutdown`](../application/application_http.go) takes `func(shutdownContext context.Context)`, handed the context the hooks are waited under, carrying the shutdown deadline. One middleware factory type remains, [`pipeline.MiddlewareFactory`](../http/middleware/pipeline/definition.go), `func(kernelInstance) (httpcontract.Middleware, error)`, renamed from `pipeline.HttpMiddlewareFactory`; `applicationcontract.MiddlewareFactory` and its alias `application.MiddlewareFactory` are removed, `MiddlewareRegistration.Factories` and `RegisterHttpMiddlewareFactories` taking it, a factory's error refusing the build naming the middleware. [`JsonHandlerErrorResponder`](../http/typed_handler.go) is `func(runtimeInstance, request, refusal JsonHandlerRefusal) (Response, error)`, `JsonHandlerRefusal{Status, Message, Cause}`.

**Symptom.** `cannot use hook (value of type func()) as func(shutdownContext context.Context) value in argument to OnHttpShutdown`; `undefined: applicationcontract.MiddlewareFactory`, `application.MiddlewareFactory`, `pipeline.HttpMiddlewareFactory`; a factory literal `func(kernel) httpcontract.Middleware` no longer fits; a five-parameter responder no longer fits `JsonHandlerConfig.ErrorResponder`.

**Remedy.** `application.OnHttpShutdown(func(shutdownContext context.Context) { … })`, waiting under the context for what the hook unwinds; a factory answers `(middleware, nil)`, or its error; a responder reads `refusal.Status`, `refusal.Message` and `refusal.Cause`.

### Bound: an exported rule is a function answering it

**What changed.** The twenty-six exported `bound.Rule` variables are functions answering the rule by value, the same name, default, bounds and unlimited admission: `config.MaxRequestBodyBytesRule()`, `StaticCacheMaxAgeRule()`, `SessionTtlRule()`, `SessionTombstoneRetentionRule()`, `SessionStorageTimeoutRule()`, `SessionMaxSessionsRule()`, `ShutdownTimeoutRule()`, `HttpReadTimeoutRule()`, `HttpReadHeaderTimeoutRule()`, `HttpWriteTimeoutRule()`, `HttpIdleTimeoutRule()`, `HttpMaxHeaderBytesRule()`, `TeardownTimeoutRule()` and `UnavailableRetryAfterRule()`; `output.OptionLimitRule()`, `OptionOffsetRule()` and `OptionTableMaxWidthRule()`; `container.ScopeTeardownTimeoutRule()` and `CreationWaitersRule()`; `lockcontract.LockTtlRule()`, `securitycontract.TokenTtlRule()` and `NonceTtlRule()`, `cachecontract.TtlRule()`, `sessioncontract.TtlRule()`, `session.StorageTimeoutRule()` and `storagecontract.PresignExpiryRule()`. A rule can no longer be reassigned for the whole process.

**Symptom.** `cannot use config.HttpReadTimeoutRule (value of type func() bound.Rule[time.Duration]) as bound.Rule[time.Duration] value`; `config.HttpReadTimeoutRule.Minimum undefined`.

**Remedy.** Call the function: `bound.Resolve(value, config.HttpReadTimeoutRule())`, `config.UnavailableRetryAfterRule().Name`; a reading of your own copies the rule into a local and changes the copy.

### Lock: `CreateLock` answers an error, and `LeaseLocker` is removed

**What changed.** [`lockcontract.Locker`](../lock/contract/lock.go)'s `CreateLock(name, ttl)` answers `(Lock, error)`: a lease locker refuses an unlimited ttl there, its cause `ErrUnlimitedTtlRefused`, the in-memory and rueidis lockers through `ResolveLeaseTtl`, and the mysql and pgsql lockers answer a nil error for any ttl. `LeaseLocker` and `LeaseOnly()` are removed: `NewLeaderGate` and `NewExclusiveCommand` create the lock once at construction and panic with its refusal, so a decorated locker is refused there too, where it lost the mark and was refused at the first `Acquire`; the lazy locker answers its inner locker's refusal from the first `Acquire`, `Release` or `Refresh`; `RunExclusive` and the outbox relay answer it as their error.

**Symptom.** `assignment mismatch: 1 variable but locker.CreateLock returns 2 values`; `*L does not implement lockcontract.Locker (wrong type for method CreateLock)`; `undefined: lockcontract.LeaseLocker`.

**Remedy.** `lock, createErr := locker.CreateLock(name, ttl)` and handle the error; a locker of the application's own answers `(lock, nil)`, a lease locker refusing an unlimited ttl through `lockcontract.ResolveLeaseTtl(ttl)`, and drops `LeaseOnly()`.

### Mailer: `Send` answers a receipt

**What changed.** [`mailercontract.Mailer`](../mailer/contract/mailer.go)'s and `Transport`'s `Send` answer `(Receipt, error)`: `Receipt.MessageId` is the `Message-ID` the smtp transport wrote, the caller's own header trimmed or the one it minted, one value with the header; the in-memory and log transports answer the zero receipt; `Manager.Send` answers its transport's, the zero receipt beside any error. A caller's `Message-ID` header is written trimmed.

**Symptom.** `assignment mismatch: 1 variable but mailer.Send returns 2 values`; `*T does not implement mailercontract.Transport (wrong type for method Send)`.

**Remedy.** `receipt, sendErr := mailer.Send(runtimeInstance, message)`, or `_, sendErr :=`; a transport of the application's own answers `mailercontract.Receipt{MessageId: id}` or the zero receipt.

### Clock: a clock arms timers

**What changed.** [`clockcontract.Clock`](../clock/contract/clock.go) carries `NewTimer(duration) Timer`, a `Timer` of `Channel()`, `Stop() bool` and `Reset(duration) bool` answering as `time.Timer`'s do since Go 1.23: a value not yet received is dropped by `Stop` and `Reset`, so `Reset` needs no drain. The frozen clock's timer runs on wall time and sends its frozen `Now`, as its ticker does, and follows the same `Stop` and `Reset` rule.

**Symptom.** `*C does not implement clockcontract.Clock (missing method NewTimer)` for a clock of the application's own, a test clock included.

**Remedy.** Add `NewTimer`; a test clock with no timeline of its own answers `clock.NewSystemClock().NewTimer(duration)`.

### Validation: a constraint receives a validation context, and the validator takes the runtime

**What changed.** [`validationcontract.Constraint`](../validation/contract/constraint.go) is `Validate(validationContext ValidationContext, value any) ValidationError`, where it took `(value, field)`: the context carries `Runtime`, `Field`, the path the error reports, and `Owner`, the struct that holds the field as a value, so a cross-field rule reads its sibling. [`Validator.Validate(runtimeInstance, data)`](../validation/validator.go) takes the runtime its constraints are handed, a nil one refused with "the runtime is nil or carries no context"; `Request.BindJsonAndValidate` and the json handler hand the request's, and answer a validator error that is no `ValidationErrors` as a server failure (500), the client's 400 kept for validation errors. A fresh validator registers `min`, `max`, `regex`, `greaterThan` and `lessThan` as zero templates.

**Symptom.** `*C does not implement validationcontract.Constraint (wrong type for method Validate)`; `not enough arguments in call to validator.Validate`.

**Remedy.** Write a constraint as `func (c *C) Validate(validationContext validationcontract.ValidationContext, value any) validationcontract.ValidationError`, reading `validationContext.Field` where it read `field`; call `validator.Validate(runtimeInstance, data)` with the runtime the code holds.

### Messagebus, openapi: one constructor per command, and the retry policy has one source

**What changed.** [`messagebus.NewConsumeCommand(config)`](../messagebus/consume_command.go) is the body of the removed `NewConsumeCommandFromContainer`: it resolves the bus and the transports from the run's scope; the eager `NewConsumeCommand(bus, transports, config)` is removed. [`openapi.NewGenerateCommand()`](../openapi/generate_command.go) resolves the `Info` and the `Registry` the same way, the eager `NewGenerateCommand(info, registry)` and `NewGenerateCommandFromContainer` removed. `messagebus.ServiceRetryPolicy` and `RetryPolicyFromResolver` are removed: the retry policy of the framework's consume command is [`ApplicationConfig.MessageBus`](../application/application_new.go)`.Retry`, a `MessageBusConfig`, validated by `NewApplication`, where a policy registered in the container won over the command's config.

**Symptom.** `too many arguments in call to messagebus.NewConsumeCommand`, `undefined: messagebus.NewConsumeCommandFromContainer`, `messagebus.ServiceRetryPolicy`, `messagebus.RetryPolicyFromResolver`, `openapi.NewGenerateCommandFromContainer`; at run time a retry policy registered in the container changes nothing.

**Remedy.** Register the bus and the transports in the container (`messagebus.RegisterTransports`) and build `messagebus.NewConsumeCommand(messagebus.ConsumeCommandConfig{…})`, or let the framework register it; move a registered retry policy to `application.ApplicationConfig{MessageBus: application.MessageBusConfig{Retry: policy}}`; write `openapi.NewGenerateCommand()`. Name the failure transport with `RetryPolicy.FailureTransportName`, the name it is registered under in `RegisterTransports`. A policy the container still carries under `service.messagebus.retry_policy` is read by nothing and named in the `messagebus.retryPolicyServiceIgnored` boot warning.

### Messagebus: `Receive` answers a subscription, `Nack` takes a config

**What changed.** [`messagebuscontract.Transport`](../messagebus/contract/transport.go)'s `Receive(runtime)` answers a `Subscription` of `Envelopes()` and `Err()`, where it answered a channel, and `Nack(runtime, envelope, NackConfig{Requeue})` takes a config where it took a bool. The consume command reads `Err()` when the stream ends without the stop: the reason travels as the cause of `transport delivery channel closed`. The amqp transport's subscription names the connection gone with no dialer or the channel it could not reopen; the in-memory one answers nil, and each of its subscriptions closes its stream when the runtime handed to `Receive` ends, the transport's queue staying open for the others, a delivery the stopped subscription took going back to the queue.

**Symptom.** `*T does not implement messagebuscontract.Transport (wrong type for method Receive)` or `(wrong type for method Nack)`; `cannot range over queue` or `cannot receive from non-channel` on a `Receive` answer; `cannot use true (untyped bool constant) as messagebuscontract.NackConfig value`.

**Remedy.** A transport of the application's own answers a `Subscription` whose `Envelopes()` is its channel and whose `Err()` says why it closed, nil for a stop; read `subscription.Envelopes()` where the channel was read; write `Nack(runtime, envelope, messagebuscontract.NackConfig{Requeue: true})` for `true` and `NackConfig{}` for `false`.

### Messagebus: a handler receives the envelope; one stamping and one send door

**What changed.** [`MessageHandler.Handle`](../messagebus/contract/handler.go) is `Handle(runtimeInstance, envelope)` and the function [`RegisterHandler[T]`](../messagebus/locator.go) registers is `func(runtimeInstance, message T, envelope Envelope) error`, so a handler reads `messagebus.MessageId(envelope)` and the redelivery count of the delivery it handles. `Envelope.WithStamp` is removed beside `WithStamps`. [`NewSendMessageMiddleware`](../messagebus/middleware_send.go) takes the `*Routing` that `NewRouting` and `RouteType[T]` build; its map form, `NewSendMessageMiddlewareFromRouting` and `TransportRouting` are removed.

**Symptom.** A handler function with two parameters, a `MessageHandler` of the application's own, `envelope.WithStamp(stamp)`, `NewSendMessageMiddleware(map…)` and `NewSendMessageMiddlewareFromRouting` no longer compile.

**Remedy.** Add the envelope as the third parameter of a handler function and the second of `Handle`; write `envelope.WithStamps([]messagebuscontract.Stamp{stamp})`; build the routing with `NewRouting` and `RouteType[T]` and hand it to `NewSendMessageMiddleware`.

### Lock: one deadline for a release, the locker's

**What changed.** `ExclusiveConfig.ReleaseTimeout`, `LeaderGateConfig.ReleaseTimeout`, `ExclusiveCommandConfig.ReleaseTimeout` and the outbox's `RelayConfig.LockReleaseTimeout` are removed. The helpers and the relay release on a context detached from the run's cancellation, with no deadline of their own; [`Lock.Release`](../lock/contract/lock.go) bounds its own round trip, as the mysql and pgsql lockers do through `LockerConfig.ReleaseTimeout` and the redis one through its call timeout.

**Symptom.** A config literal naming `ReleaseTimeout` on one of the three helpers, or `LockReleaseTimeout` on the relay, no longer compiles. A `Lock` of the application's own whose `Release` waits on its context without a bound of its own can now wait until the store answers.

**Remedy.** Drop the field and set the bound on the locker; a `Lock` of the application's own bounds its `Release` itself.

### Bunorm: a provider refuses `Insecure` beside `TlsConfig`

**What changed.** The mysql and pgsql [`NewProvider`](../../integrations/bunorm/mysql/v4/provider.go) panic at construction, naming `ProviderConfig.Insecure` and `ProviderConfig.TlsConfig`, when both are set, where `TlsConfig` won in silence.

**Symptom.** A boot that sets both panics at construction; nothing fails to compile.

**Remedy.** Drop `Insecure` to dial the TLS session, or `TlsConfig` to dial plaintext.

### Opentelemetry: the otlp provider shuts down under the teardown's context

**What changed.** `otlp.ModuleConfig.ShutdownTimeout` is removed; the provider's shutdown runs under the teardown's own context, so `MELODY_TEARDOWN_TIMEOUT` is the one bound and a declared unlimited teardown stays unlimited.

**Symptom.** A `ModuleConfig{ShutdownTimeout: …}` literal no longer compiles.

**Remedy.** Drop the field; bound the flush through the application's teardown timeout.

### Translation: a quoted `#` in a plural branch, and locales matched in BCP 47 case

**What changed.** Inside a `plural` branch `'#'` is a literal `#` and `''` an apostrophe, and [`ValidatePattern`](../translation/message_format.go) refuses a `'#` no apostrophe closes. The [`Manager`](../translation/manager.go) matches every locale in BCP 47 case (`ro-ro` finds `ro-RO`, separators kept as written) and refuses two catalogs spelling one locale differently.

**Symptom.** A plural branch holding `'#'` renders `#` where it rendered `'5'`, one holding `''` renders a single apostrophe, and a catalog holding a lone `'#` in a branch fails the boot. A request locale in another case now finds its catalog where it fell through to the fallbacks. A boot registering `ro-RO` and `ro-ro` panics naming both, and a translations directory holding `messages.ro-RO.json` beside `validation.ro-ro.json` fails the load naming both files.

**Remedy.** Write `'#'` for a literal `#` in a plural branch and `''` for an apostrophe followed by `#`. Register each locale under one spelling; the request locale no longer needs normalizing to that spelling.

### Cron: a root generator refuses a logs directory its entry users cannot write

**What changed.** [`melody:cron:generate`](../../integrations/cron/v4/generate_command.go) run as root refuses a missing logs directory, and an existing one an entry's user cannot write and enter, when that entry writes its log file directly in it.

**Symptom.** A deployment whose root-run generator created the logs directory now fails naming the directory and the users.

**Remedy.** Create the logs directory beforehand, owned by or writable for the entries' users, or give those entries log file names in a subdirectory, which the generator creates and hands over. A directory managed through an access control list, and log files that already exist for their users, are not judged. A script can tell the refusal by `errors.Is(err, cron.ErrLogsDirectoryNotWritable)`.

### Bunorm encrypt: the migrator judges its specification against the schema

**What changed.** The [encrypt `Migrator`](../../integrations/bunorm/v4/encrypt/migrate.go) runs on one connection and, before the first row, matches every column and the key to the table letter case aside, proves the key unique and not NULL, and refuses a decrypt into a character column that is not `utf8mb4` under a non-strict `sql_mode`. A duplicate-entry or incorrect-string refusal from the server is reported without its message.

**Symptom.** A run over a key that is not unique, a nullable unique key, or a column name the table does not have fails before it reads a row; a decrypt into a `latin1` column under a non-strict session fails naming the column, its character set and the mode.

**Remedy.** Name a key that is the primary key or the only column of a unique `NOT NULL` index; set `STRICT_TRANS_TABLES` for the session (`sql_mode` in the DSN) or convert the column to `utf8mb4` before a decrypt.

### Http: an informational status is no final answer

**What changed.** A response whose status is between 100 and 199, 101 included, is refused before anything is written and answered as the rendered 500. net/http sends a 1xx as an interim header and answers an implicit 200 after it, so the handler's body went out as a success; a response object with 101 cannot hand the connection over, so it never upgraded anything. The refusal has no off switch, since no such response can work on either protocol.

**Symptom.** A handler that answered 100, 101, 102 or 103 as its final status now answers 500.

**Remedy.** Answer the final status the request ends with. An early hint (103) is sent through the underlying writer before the final response, not as the response. A handler that upgrades writes the 101 on the `ResponseWriter` and hijacks the connection, which is unchanged; a handler that returned a response object with 101 never upgraded anything.

### Exception: an http exception answers 400 to 599

**What changed.** [`NewHttpException`](../exception/http_exception_new.go) and `NewHttpError` panic on a status outside `[400, 599]`, where they admitted `[200, 599]`; the refusal names the status, the minimum and the maximum. An exception answers a client or a server error: a success or a redirect raised as one served the error body as a success.

**Symptom.** A constructor called with a 2xx or a 3xx status, or with a status a handler took from a remote answer and forwarded, panics where it is made.

**Remedy.** Write a success or a redirect as a response, not as an exception. A handler that forwards a remote status checks it is between 400 and 599 before it raises it, and answers any other through a response.

### Serializer: the plain-text serializer writes a structured value as lines

**What changed.** [`NewPlainTextSerializer`](../serializer/plain_text_serializer.go) takes a [`PlainTextSerializerConfig`](../serializer/plain_text_serializer.go), whose four `bound.Count` fields, `MaxDepth`, `MaxLines`, `MaxBytes` and `MaxMembers`, take 8, 1,024, 262,144 and 16,384 when left out; `MaxDepth` admits 1 to 1,024, and a bound outside its rule panics, naming the field. `Serialize` writes a map, a struct or a list as lines, where the third major wrote any value other than a string or a byte slice with `%v`: a map is one `key: value` line per entry in key order, a struct is written by the fields `encoding/json` writes under their json names, a `json.Marshaler` by the json it writes and an `encoding.TextMarshaler` by its text, a nested value is indented, a list is one `- item` line per member, a pointer is written as the value it holds, a control character in a key or a value is spelled as its escape, and the body ends with a newline. A scalar on its own is written whole as its text, a nil value as nothing, where it was `<nil>`, and a method that panics or a marshaler that fails is an error from `Serialize`. The error body a `text/plain` client reads is written by the same walk: a struct in it is written by its json names, its unexported fields left out, where it was written inline under its Go field names, a `time.Time` in RFC 3339, a map or a struct inside a list beneath its `-`, a map whose keys are not strings as entries, a pointer as the value it holds, a nil value as a bare label or a bare `-`, where it was `<nil>`, and a scalar inside a list at the depth bound is written, where it was `...`; a control character in the message, a key or a value is spelled as its escape, where a value holding a newline wrote a second line. A body of maps, strings and lists of scalars free of control characters is unchanged byte for byte within its bounds.

**Symptom.** A call to `NewPlainTextSerializer()` does not compile. A `text/plain` client of a handler returning a map, a struct or a slice reads lines, where it read a Go dump such as `map[id:7 name:ana]`, `{7 ana}` or `[1 2]`; a client that parsed that dump, an error body's inline struct, or a multi-line value of an error body finds the lines, the value on one line with its newlines escaped.

**Remedy.** Write `NewPlainTextSerializer(serializer.PlainTextSerializerConfig{})` for the defaults, or set a bound. A client that needs a value's exact Go spelling is served a string the handler formats itself. A struct whose plain-text form must leave a field out tags it `json:"-"` or writes its own json, as it does for json.

### Application: a module registers once per application

**What changed.** A module's [`Name()`](../application/contract/module.go) is its identity in the application. [`RegisterModule`](../application/application_module.go) and `RegisterModuleProvider` record a second module of a name already registered, the same instance reached again through a second provider included, at the line that registered it, and `Boot` fails at its start with the aggregated report of duplicate registrations, the module listed as `module "<name>"`, before any phase can fail over what the duplicate would have wired; the duplicate runs no hook and the modules it provides are not expanded, so the report names the cause once. A module whose `Name()` is empty is refused with a panic where it is registered. The identity was the instance: two distinct instances sharing a name booted as two modules, each running every hook, and a module registered as an uncomparable value was never deduplicated.

**Symptom.** A boot that registers one integration twice, or two modules of the application that answer the same name, fails with `duplicate registrations detected at boot` naming `module "<name>"` and the line of the second registration. Two websocket endpoints, registered as two websocket modules, are such a duplicate. An opentelemetry module registered twice ran its middlewares and decorators twice without a word.

**Remedy.** Register each module once. A module of the application that is registered with two configurations is one module carrying both, or two modules of two names. The websocket module declares every endpoint in `ModuleConfig.Streams`, and the encrypt module declares every compartment in `Contexts` and every bound column in `SealBindings`, of one `ModuleConfig`. Two providers that share a module stop carrying it, and the composition root registers it once.

**Why.** A module is a piece of the application's wiring: a second registration either collided on every service, parameter and command it registers, scattered across the report under names that hid the cause, or, for a module whose hooks register nothing by name, ran them again silently: middlewares installed twice, a process-wide table half overwritten.

### Application: a module name carries no padding, and two names differing only by case are one module

**What changed.** [`RegisterModule`](../application/application_module.go) and `RegisterModuleProvider` refuse, with a panic where it is registered, a module whose `Name()` begins or ends with whitespace, naming the module's type and the quoted name. The identity of a module is its name folded to lower case: a module named `Twin` after one named `twin` is a duplicate, recorded in the aggregated boot report as `module "Twin" registered at <line>, already registered as "twin"`, and neither it nor the modules it provides boots. The first module keeps its own spelling everywhere else.

**Symptom.** A boot that registers a module answering `"cache "` fails with `module name may not begin or end with whitespace`; two modules whose names differ only by case fail the boot with `duplicate registrations detected at boot`, where both booted.

**Remedy.** Answer the name without padding, and give two modules two names that differ by more than case. Every module the framework ships names itself in lower case and is unaffected.

### Http: `ErrorUnsupportedContentType` and `ErrorJsonBodyHasExtraData` are removed

**What changed.** The two error constructors of the [`http`](../http/request.go) package are removed. No door of the framework answered either.

**Symptom.** Code that calls them no longer compiles.

**Remedy.** Build the error where it is answered: `exception.NewError("unsupported content type", nil, nil)` and `exception.NewError("json body has extra data", nil, nil)`.

### Cron: `Entry.Args` is renamed `Entry.Arguments`, and `ErrSharedRunnerCommandFlags` is removed

**What changed.** The field of [`cron.Entry`](../../integrations/cron/v4/entry.go) that holds a command's arguments is `Arguments`; the k8s template's refusal of an empty token names the field `Arguments`. `ErrSharedRunnerCommandFlags`, deprecated and answered by nothing, is removed. Nothing else changes.

**Symptom.** A custom template reading `entry.Args`, or code matching `ErrSharedRunnerCommandFlags`, no longer compiles.

**Remedy.** Read `entry.Arguments`; drop the match on `ErrSharedRunnerCommandFlags`, or match `ErrNilRunnerCommandFlag` if the check meant a flag the runner refuses.

### Migrate: `Query.SQL` is renamed `Query.Sql`

**What changed.** The statement field of [`migrate.Query`](../../integrations/bunorm/migrate/v4/migrate.go) is `Sql`. Nothing else changes.

**Symptom.** A migration that builds `migrate.Query{Name: ..., SQL: ...}` no longer compiles.

**Remedy.** Write `Sql:`.

### Cli: the application's version is `ApplicationConfig.Version`, and the root shows `--version`

**What changed.** The application's version has one source, [`ApplicationConfig.Version`](../application/application_new.go). The application declares it on the root of its command tree: the root shows `--version`, printing `<name> version <version>`, `<unknown>` when none is declared, and every command document's meta carries it, `""` when undeclared. `output.SetApplicationVersion`, a process-wide value the last application to call it won, and `debug.VersionCommand.ApplicationVersion` are removed. [`cli.DispatchCommand`](../cli/dispatch.go) takes a `DispatchConfig` last, whose `ApplicationVersion` the dispatched command reads.

**Symptom.** A call to `output.SetApplicationVersion`, a `VersionCommand{ApplicationVersion: …}` literal and a `DispatchCommand` call with five arguments no longer compile. An `output.Version{Application: …}` handed to `output.NewMeta` still compiles and is no longer read: the meta carries `Option.ApplicationVersion`. A root command of the application answers `--version` where it refused the flag; `-v` stays the verbosity, never an alias of `--version`, and the root's help prints a `VERSION:` block, `<unknown>` when none is declared.

**Remedy.** Write the version in the configuration, `application.NewApplication(ctx, application.ApplicationConfig{Version: version, …})`, and drop the call and the field. Pass `cli.DispatchConfig{}` to `DispatchCommand`, or `cli.DispatchConfig{ApplicationVersion: output.ApplicationVersionOf(commandContext)}` when dispatching from inside a command, so the dispatched command's documents carry the version.

### Migrate: the two migration tables are named by `.env` keys

**What changed.** The [migrate module](../../integrations/bunorm/migrate/v4/README.md) declares `MELODY_BUNORM_MIGRATE_MIGRATIONS_TABLE` and `MELODY_BUNORM_MIGRATE_LOCKS_TABLE`, the tables bun keeps, `bun_migrations` and `bun_migration_locks` when unset; `ModuleConfig.KeyDefaults` sets only their defaults. A value that is not a plain identifier of at most 64 bytes fails the boot with `a declared environment key is refused`, the error's context naming the key; two keys naming one table fail the command with `migrate: the migrations table and the locks table are one table`.

**Symptom.** None for an application that sets neither key: the commands act on the tables they acted on. An application whose own `.env` already defines a variable of either name has it read as the table name.

**Remedy.** Nothing to do to keep bun's tables. To name them, set the keys in the `.env` files, or `KeyDefaults` for the application's own default.

The defaults stay bun's own names rather than `melody_*`: a renamed default would make `db:migrate` on an existing database see no history and replay every migration; the two keys are the door for an application that wants the prefix.

### Audit: a `FileStorage` line names its keys in camelCase, and `changes` is an array

**What changed.** A line [`audit.FileStorage`](../../integrations/bunorm/v4/audit/storage.go) appends names every key in camelCase and embeds the entry's changes as the json array they are:

```json
{"table":"melody_audit","entry":{"id":1,"transactionId":7,"entity":"product","entityId":"42","operation":"UPDATE","changes":[{"field":"name","old":"a","new":"b"}],"actor":"ana","createdAt":"2026-11-13T09:00:00Z"}}
```

where the third major wrote the entry's Go field names and the changes as an escaped string:

```json
{"table":"melody_audit","entry":{"Id":1,"TransactionId":7,"Entity":"product","EntityId":"42","Operation":"UPDATE","Changes":"[{\"field\":\"name\",\"old\":\"a\",\"new\":\"b\"}]","Actor":"ana","CreatedAt":"2026-11-13T09:00:00Z"}}
```

A `Changes` that is not a json array, an `Entry` built by hand, is written as a json string. `audit.Entry` itself carries no json tags, so an application marshalling it in its own documents is unaffected.

**Symptom.** A reader of the audit file that looks up `entry.Changes`, or decodes `changes` as a string, finds nothing or fails. A file the third major wrote keeps its lines, so a reader of a file that spans the upgrade meets both forms.

**Remedy.** Read the camelCase keys, and `changes` as an array; tell a hand-built value by its token being a string. No converter ships: a reader that must read old files tries both forms.

### Object storage: the aws/s3 module is named `aws.s3`

**What changed.** The [aws/s3 module](../../integrations/aws/s3/v4/README.md) answers `aws.s3` from `Name()`, declares its boot warning as `aws.s3.plaintextEndpoint`, exported as `PlaintextEndpointBootWarningName`, and a `Config` prints as `aws.s3.Config{…}`, after the module's path `integrations/aws/s3/v4`, where all three said `awss3`. The Go package is still `awss3`.

**Symptom.** A journal filter on `bootWarning=awss3.plaintextEndpoint`, the code the third major wrote, matches nothing. A `MELODY_CLI_BOOT_WARNINGS_IGNORED` that names the old code fails the boot with `cli boot warning code names no boot warning the framework or a module writes`.

**Remedy.** Write `aws.s3.plaintextEndpoint` in `MELODY_CLI_BOOT_WARNINGS_IGNORED` and in any journal filter.

### Http: `Request.Param` and `Request.Params` are renamed `Parameter` and `Parameters`

**What changed.** [`httpcontract.Request`](../http/contract/request.go) and the framework's request answer a route parameter through `Parameter(name)` and all of them through `Parameters()`, and [`httpcontract.MatchResult`](../http/contract/router.go) holds them in `Parameters`. Nothing else changes.

**Symptom.** A handler calling `request.Param("id")` or `request.Params()`, a request of the application's own implementing the contract, and code reading `MatchResult.Params` no longer compile.

**Remedy.** Write `request.Parameter("id")`, `request.Parameters()` and `MatchResult.Parameters`.

### Bunorm: `ProviderDefinition.Params` is renamed `Parameters`

**What changed.** The connection values of a [`bunorm.ProviderDefinition`](../../integrations/bunorm/v4/provider_definition.go) are `Parameters`. Nothing else changes.

**Symptom.** A definition built as `bunorm.ProviderDefinition{..., Params: bunorm.ConnectionParameters{...}}` no longer compiles.

**Remedy.** Write `Parameters:`.

### Encrypt: `CipherRef` is `CipherReference` and `TableSpec` is `TableSpecification`

**What changed.** In the [encrypt](../../integrations/bunorm/v4/encrypt/README.md) package the marker interface `CipherRef` is `CipherReference` and the run description `TableSpec` is `TableSpecification`. A refusal naming a marker carries it under the context key `cipherReference`, where it was `cipherRef`, and a refusal of a run names its field `TableSpecification.<Field>`. A stored seal is untouched.

**Symptom.** `undefined: encrypt.CipherRef` and `undefined: encrypt.TableSpec`. A log query or a check on the context key `cipherRef` matches nothing.

**Remedy.** Write `encrypt.CipherReference` in constraints of your own and `encrypt.TableSpecification{...}` for a programmatic run, and read `cipherReference`.

### Migrate: `InitCommand` is renamed `InitializeCommand`

**What changed.** The [migrate](../../integrations/bunorm/migrate/v4/command_init.go) command that creates the migration tables is built by `NewInitializeCommand` and typed `InitializeCommand`. The command keeps its name on the command line.

**Symptom.** `undefined: migrate.NewInitCommand` or `undefined: migrate.InitCommand`.

**Remedy.** Write `NewInitializeCommand` and `InitializeCommand`.

### Cron: the logs directory is spelled out, flag and parameter, and `ForbiddenCharacter.Char` is `Character`

**What changed.** The [cron](../../integrations/cron/v4/README.md) generator reads the directory its entries redirect into from the flag `--logs-directory` and the parameter `melody.cron.logs_directory`, exported as `ParameterLogsDirectory`; the refusal of a missing one is `ErrNoLogsDirectory`, its message saying "no logs directory is configured", and a log file name escaping the directory is reported under the context key `logsDirectory`. `ForbiddenCharacter.Char` is `ForbiddenCharacter.Character`. The default is `%kernel.logs_directory%/cron`, the kernel parameter spelled out in its own section below.

**Symptom.** `melody:cron:generate --logs-dir …` is refused by the command line with `flag provided but not defined: -logs-dir`. A parameter the application still sets as `melody.cron.logs_dir` refuses the boot naming its successor, as [a retired key](#config-a-retired-key-refuses-the-boot) does, and so does a `.env` line `MELODY_CRON_LOGS_DIR`, a spelling of the unreleased fourth major. Code naming `ParameterLogsDir`, `ErrNoLogsDir` or `ForbiddenCharacter{Char: …}` no longer compiles.

**Remedy.** Rename the flag in the deployment's invocation, set the directory through `melody.cron.logs_directory` or the module's key `MELODY_CRON_LOGS_DIRECTORY`, and rename the Go names in code.

### Cache: every operation takes the runtime first, userland calls a bound view, and rueidis `ConnWriteTimeout` is `ConnectionWriteTimeout`

**What changed.** Every operation of [`cachecontract.Cache`](../cache/contract/cache.go) takes the runtime it runs under first: `Get(runtimeInstance, key, target)` (see [a read names its type](#cache-a-read-names-its-type)), `Set(runtimeInstance, key, value, ttl)`, `Delete`, `Has`, `Clear(runtimeInstance)`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment` and `Decrement`, and [`Manager.GetCounter`](../cache/manager.go) with them; a nil runtime, a typed nil and one carrying no context are refused with `the runtime is nil or carries no context` before anything else is judged. Userland calls them through [`cache.Bound`](../cache/bound.go), the cache bound to one runtime, whose methods take no runtime: `cache.FromRuntime(runtimeInstance)` resolves and binds the application's cache, `cache.For(runtimeInstance, cacheInstance)` binds one a service holds, and the typed doors take the view, `cache.Get[T](boundCache, key)`, `cache.Many[T](boundCache, keys)` and `cache.Remember[T](boundCache, key, config, callback)`. The wiring generator refuses a constructor argument of type `cache.Bound`. Every operation of [`cachecontract.Backend`](../cache/contract/backend.go) takes the runtime too: `Get(runtimeInstance, key)`, `Set(runtimeInstance, key, payload, ttl)` and the rest under the same names; an absent runtime is refused with the same error before anything else is judged, the closed backend included, and a runtime whose context is already done answers that context's error once the arguments are judged, before the store is touched; the manager hands its backend the runtime it was handed, and `cachecontract.RefuseNilContext` is removed. The [rueidis cache `Backend`](../../integrations/rueidis/v4/cache/backend.go) and `BackendService` carry the same doors, `ClearByPrefix(runtimeInstance, prefix)` beside them: the context-less doors the third major deprecated and the `…Ctx` doors are gone, `BackendService.WithContext` is removed, `NewBackend` takes the client and the config, and `BackendFromRuntime` answers the service's backend. `BackendConfig.CallTimeout` bounds every round trip on top of the runtime's context, the earlier deadline winning. `Remember` reads and stores without the cancellation of the context each runs under, so it answers as before. [`ClientConfig.ConnWriteTimeout`](../../integrations/rueidis/v4/client_config.go) is `ClientConfig.ConnectionWriteTimeout`.

**Symptom.** `cache.Get(key)`, `cacheInstance.Get(ctx, key, &target)`, which answers `cannot use ctx (variable of interface type context.Context) as "github.com/precision-soft/melody/v4/container/contract".Runtime value in argument to cacheInstance.Get: context.Context does not implement "github.com/precision-soft/melody/v4/container/contract".Runtime (missing method Context)`, `cache.Get[T](ctx, cacheInstance, key)`, which answers `too many arguments in call to cache.Get[T]`, `backend.Get(ctx, key)`, `backend.GetCtx(ctx, key)`, `service.WithContext(ctx)`, `NewBackend(ctx, client, config)` and a `ClientConfig{ConnWriteTimeout: …}` literal no longer compile; a cache or backend of the application's own declaring `Get(key string)` no longer satisfies the contract. A redis call under a context with a longer deadline than the call timeout is cut at the call timeout.

**Remedy.** Bind the cache once where the runtime is in hand and call the view, `products := cache.FromRuntime(runtimeInstance)` then `products.Get(key, &product)`, or `cache.Get[T](cache.For(runtimeInstance, instance.cache), key)` in a service holding the cache; a method that read a context to reach the cache takes the runtime instead. A call that must outlive the caller's cancellation binds over `runtime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context()))`. Declare the runtime first on a cache of the application's own, refusing an absent one through `containercontract.RefuseNilRuntime`. Pass the runtime as the first argument of every backend call; replace `GetCtx` and its siblings with the plain names; drop the context from `NewBackend` and the `WithContext` call; declare the runtime first on a backend of the application's own, refusing an absent one through `containercontract.RefuseNilRuntime` and reading `runtimeInstance.Context()` where it read the context; and write `ConnectionWriteTimeout:`. A call that must outlive the call timeout raises `BackendConfig.CallTimeout`.

### Session: every operation takes the runtime first, and a storage reads its ttl through one rule

**What changed.** Every door of [`sessioncontract.Manager`](../session/contract/manager.go) and [`sessioncontract.Storage`](../session/contract/storage.go) takes the runtime it runs under first: `Session(runtimeInstance, id)`, `NewSession(runtimeInstance)`, `SaveSession(runtimeInstance, session)`, `DeleteSession(runtimeInstance, id)`, `RegenerateSession(runtimeInstance, session)`, and `Load(runtimeInstance, id)`, `Save(runtimeInstance, id, data, ttl)`, `Delete(runtimeInstance, id)`; [`InMemoryStorage.Clear(runtimeInstance)`](../session/in_memory_storage.go) with them, every call reading the runtime's context. A nil runtime, a typed nil and one carrying no context are refused with `the runtime is nil or carries no context` before anything else is judged, and `sessioncontract.RefuseNilContext` is removed; a storage then judges a closed storage, the id and the ttl, in the order the cache contract judges them, and answers a context already done its own error before the store is touched; the manager answers a context that is done, or that ends while it waits for a session another operation holds, an error marked as an infrastructure failure carrying the context's error. `Storage.Save` takes a [`bound.Duration`](../bound/limit.go) read through `sessioncontract.ResolveTtl`: an absent ttl is refused and unlimited is `bound.UnlimitedDuration()`, where `0` meant no expiry. `Manager` declares a sixth method, `StorageTimeout() bound.Duration`, the one bound of every call to the storage the kernel makes, which the http kernel judges by the rule of `ManagerConfig.StorageTimeout`: absent is 5s, unlimited is no bound, and a value below 1ms fails the http boot, judged once on the registered manager before the server listens. The http kernel loads the session and rotates it on the request's context, and saves or deletes it at the end of the request detached from the request's cancellation, bounded by `MELODY_HTTP_SESSION_STORAGE_TIMEOUT` (`ManagerConfig.StorageTimeout`), 5s when left out, `unlimited` for no bound. The load of the session, the mint after a miss and the rotation run on the request's context under the same bound. A session save outage at the end of a request, the bound cutting it included, now answers an empty `503` with `Retry-After`, where it answered the empty `500`; a save that failed for a reason that is no outage keeps the empty `500` (see "Session: one `StorageTimeout` bounds every storage call, and a failed save or logout answers by its cause").

**Symptom.** `manager.Session(id)`, `manager.SaveSession(session)`, `storage.Save(id, data, time.Hour)` and a method value such as `manager.DeleteSession` handed to a `func(string) error` no longer compile; a manager or a storage of the application's own no longer satisfies the contract. A storage that received `0` for sessions that never lapse receives an unlimited `bound.Duration`. A closed storage handed an empty id answers `session storage is closed` where it answered the id refusal. The save and the logout's delete still run when the client hung up, now bounded by five seconds, a first read of the session ends when the client goes away and is recorded at warning, and a load, a rotation or a save against a storage that does not answer ends after five seconds as the `503` where it held the request; a client or a monitor that read the `500` of a save outage reads a `503` with `Retry-After`.

**Remedy.** Pass the request's runtime, `runtimeInstance` in a handler, as the first argument, and a runtime derived by `runtime.WithContext(runtimeInstance, ctx)` where a call needs another deadline; declare the runtime first on an implementation of the application's own, refuse an absent one through `containercontract.RefuseNilRuntime` and read `runtimeInstance.Context()` where the context was read, judge a closed storage before the arguments, read the ttl through `sessioncontract.ResolveTtl` and honour the context in every round trip; write `bound.Of(…)` or `bound.UnlimitedDuration()` where a duration or `0` was passed; wrap a method value in a closure over the runtime; a manager of the application's own declares `StorageTimeout()`, `bound.Duration{}` for the default, and a decorator forwards the decorated manager's; a deployment whose storage legitimately takes longer raises the key.

### Outbox: an unreachable transport charges no attempt

**What changed.** A send the transport marks with `exception.MarkInfrastructureFailure`, which the amqp transport now does for every publish failure but the broker's verdict on the message, the caller's own cancellation and a closing transport, hands the row back through the new [`Repository.RescheduleUncharged`](../../integrations/outbox/v4/repository.go) once the next row of the batch, sent as a probe, failed too (a delivered probe charges the failed row as before): its attempts unchanged, the claim's delivery attempt taken back, pending again after [`RelayConfig.UnavailableBackoff`](../../integrations/outbox/v4/relay.go) (15s, growing per consecutive outage pass) capped by its `Max` (1m).

**Symptom.** A `Repository` of the application's own no longer satisfies the interface. Through a broker outage the rows are no longer dead-lettered, and the relay writes one ERROR record when the outage starts instead of one per failed send.

**Remedy.** Implement `RescheduleUncharged` as the store does: the row pending at `availableAt`, `attempts` untouched, `delivery_attempts` minus one but never below zero, the row handed back even at zero (no `delivery_attempts > 0` filter), fenced on the claim token. A transport of the application's own marks an outage with `exception.MarkInfrastructureFailure` to get the same accounting. A marked amqp `Send` failure reaching an http handler is answered 503 with Retry-After where it was a 500.

### Application: a second `Run` panics, and the exit line carries the cause chain

**What changed.** A second [`Application.Run`](../application/application.go) panics with `application run called twice` before anything is booted or marked serving. The line [`LogOnRecoverAndExit`](../logging/recover.go) echoes to stderr before a non-zero exit and a failed command's `[error]` line append the cause chain after the message, `: cause 1: …: cause 2: …`: up to the logger's `CauseChainDepth` causes on the exit line and 8 on the command's, each escaped to one line and cut at 512 bytes, a link the text before it ends with (a `%w` wrapper's repeat) left out.

**Symptom.** A `main` that called `Run` twice panics on the second call. Tooling that matches the whole stderr line sees the causes after the message.

**Remedy.** Call `Run` once and return from `main`; match the line's prefix, which is unchanged.

### Config: the kernel's directories are spelled out, Go names, parameters and the public directory key

**What changed.** The [kernel configuration](../config/contract/kernel.go) answers `ProjectDirectory()`, `LogsDirectory()` and `CacheDirectory()`, and the [http configuration](../config/contract/http.go) `PublicDirectory()`. The parameters are `kernel.project_directory`, `kernel.logs_directory`, `kernel.cache_directory` and `kernel.public_directory`, exported as `KernelProjectDirectory`, `KernelLogsDirectory`, `KernelCacheDirectory` and `KernelPublicDirectory`, and the public directory's key is `MELODY_PUBLIC_DIRECTORY`, exported as `PublicDirectoryKey`. The resolved directories are unchanged, and so are the cron module's defaults, which now name the new parameters. A refusal or a journal line naming the public directory carries it under `publicDirectory`.

**Symptom.** A value naming `%kernel.logs_dir%` (or any of the three others) refuses the boot, its cause chain ending in `undefined parameter key in template`: a framework key, `MELODY_LOG_PATH` measured, with `could not initialize the configuration`, a parameter of the application's own or a `melody.cron.*` value at `Boot` with `could not resolve the config parameters on boot`. A `.env` still setting `MELODY_PUBLIC_DIR` refuses the boot with `the environment key MELODY_PUBLIC_DIR is retired: rename it to MELODY_PUBLIC_DIRECTORY`, as [a retired key](#config-a-retired-key-refuses-the-boot) does. Code calling `ProjectDir()`, `LogsDir()`, `CacheDir()` or `PublicDir()`, or naming `KernelProjectDir`, `KernelPublicDir` or `PublicDirKey`, no longer compiles.

**Remedy.** Write `%kernel.project_directory%`, `%kernel.logs_directory%`, `%kernel.cache_directory%` and `%kernel.public_directory%`, rename the key to `MELODY_PUBLIC_DIRECTORY`, and rename the Go names; a journal query on `publicDir` reads `publicDirectory`.

### Config: the environment's Go names are spelled out; `MELODY_ENV` is unchanged

**What changed.** `config.EnvKey`, `KernelEnv`, `EnvDevelopment` and `EnvProduction` are `EnvironmentNameKey`, `KernelEnvironment`, `EnvironmentDevelopment` and `EnvironmentProduction`; the [kernel configuration](../config/contract/kernel.go)'s `Env()` is `Environment()`, and [`MiddlewareBuildReport`](../http/middleware/pipeline/report.go)'s `KernelEnv()` is `KernelEnvironment()`. The key `MELODY_ENV`, its values `dev` and `prod` and the parameter `kernel.environment` are unchanged.

**Symptom.** `undefined: config.EnvDevelopment` and the same for its siblings; `Kernel().Env undefined`.

**Remedy.** Rename the Go names; nothing changes in `.env`.

`MELODY_ENV`, `dev` and `prod` stay abbreviated on purpose: they are operator-facing strings in every deployed `.env`, where a rename would be a silent misconfiguration, an unknown value, while a renamed Go name fails to compile.

### Httpclient: the idle pool's fields are spelled out

**What changed.** [`TransportConfig`](../httpclient/transport_config.go)'s `MaxIdleConns`, `MaxIdleConnsPerHost` and `IdleConnTimeout` are `MaxIdleConnections`, `MaxIdleConnectionsPerHost` and `IdleConnectionTimeout`, beside `IdleConnectionsDisabled`, and a refusal names the new field. The values still reach net/http's own fields.

**Symptom.** A `TransportConfig{MaxIdleConns: …}` literal no longer compiles.

**Remedy.** Write `MaxIdleConnections:`, `MaxIdleConnectionsPerHost:` and `IdleConnectionTimeout:`.

### Openapi: `SpecHandler` is `SpecificationHandler`, `ExternalDocs` and `Ref` are spelled out

**What changed.** The handler serving the document is [`SpecificationHandler`](../openapi/spec_handler.go); the [document](../openapi/document.go)'s `ExternalDocs` type and field are `ExternalDocumentation`, and its `Schema.Ref` is `Schema.Reference`. The generated document is byte-identical: its keys stay `externalDocs` and `$ref`.

**Symptom.** `undefined: openapi.SpecHandler`; a `Schema{Ref: …}` or `Document{ExternalDocs: …}` literal no longer compiles.

**Remedy.** Rename the Go names.

### Validation: `WithParams` is `WithParameters`

**What changed.** [`ParameterizedConstraint`](../validation/contract/constraint.go) declares `WithParameters(parameters map[string]string)`, and the framework's constraints implement it under that name. A refusal of a rule's parameters names them under the context key `parameters`, where it said `params`.

**Symptom.** A constraint of the application's own that still implements `WithParams` compiles and is no longer a parameterized constraint: a rule naming it with parameters fails closed, `constraint does not accept parameters`, and a rule naming it without parameters is no longer refused with `constraint requires parameters` but validates with the registered instance, silently. A call to `WithParams` no longer compiles.

**Remedy.** Rename the method to `WithParameters`; a journal query on `params` reads `parameters`.

### Security: the internal-auth names are spelled out; the envelope is unchanged

**What changed.** The internal-auth HMAC names say `Application` where they said `App`: [`HmacKey.Application`](../security/hmac_secret_provider.go), [`HmacEnvelopeSignerConfig.Application`](../security/hmac_signer.go), [`HmacTokenSourceConfig.Applications`](../security/hmac_token_source.go), [`HmacApplicationRegistry`](../security/hmac_application_registry.go) with `RolesForApplication`, `StaticHmacApplicationRegistry` and `NewStaticHmacApplicationRegistry`, and `HmacSecretProvider.ApplicationForKeyId`. The refusals say "application" and name it under `application`, `claimedApplication`, `boundApplication`, `signerApplication` and `otherApplication`. The signed envelope keeps its json field `app`, so a fourth-major service and a third-major one verify each other.

**Symptom.** `undefined: security.NewStaticHmacAppRegistry`; an `HmacKey{App: …}` or `HmacTokenSourceConfig{Apps: …}` literal no longer compiles; a secret provider of the application's own that implements `AppForKeyId` no longer satisfies `HmacSecretProvider`. A journal query on `claimedApp` matches nothing.

**Remedy.** Rename the Go names and the method of a provider of your own; read the context keys under their new names.

The wire key stays `app` on purpose: a fleet upgrades service by service, and a v3 signer must verify on a v4 verifier and back, so the wire format is frozen across majors while the Go spelling is not.

### Mailer: `SmtpConfig.RequireAuth` is `RequireAuthentication`

**What changed.** [`SmtpConfig`](../mailer/smtp_transport.go)'s `RequireAuth` is `RequireAuthentication`; its meaning is unchanged.

**Symptom.** An `SmtpConfig{RequireAuth: true}` literal no longer compiles.

**Remedy.** Write `RequireAuthentication: true`.

### Rueidis: `ClientConfig.SelectDb` is `SelectDatabase`

**What changed.** [`ClientConfig`](../../integrations/rueidis/v4/client_config.go)'s `SelectDb` is `SelectDatabase`, and the connection's journal context names it `selectDatabase`. The value still reaches the library's `SelectDB`.

**Symptom.** A `ClientConfig{SelectDb: 2}` literal no longer compiles.

**Remedy.** Write `SelectDatabase: 2`.

### Application: a module declares the `.env` keys it owns

**What changed.** A module can declare its keys through [`EnvironmentKeyModule`](../application/contract/environment_key_module.go): `RegisterEnvironmentKeys(registrar)` runs first in `Boot`, before any parameter hook, and hands one [`EnvironmentKey`](../config/contract/environment_key.go) per key — the `MELODY_<MODULE>_<NAME>` key, the parameter its code reads, the field a refusal names, the default, the secret mark and a rule built with `LimitKeyRule`, `SwitchKeyRule` or `ChoiceKeyRule`. The parameter reads the key through `%env(KEY)%` and takes the default when no `.env` file sets the key; after the resolve each declared key is judged on its resolved value in every mode, as the framework's keys are. A module's key had no default, no rule, no secret mark and no refusal naming it: the application mapped it by hand onto the module's parameter, and a value the module could not use was found at its first read.

**Symptom.** None for an application whose modules declare nothing. An application that registers, before `Boot`, the parameter a module now declares is refused with `the parameter a declared environment key feeds is already registered`, naming the key, the parameter and the module; a parameter hook that registers it is the duplicate of the aggregated boot report. A configuration double that stands in for [`Configuration`](../config/configuration.go) beside a module that declares keys is refused naming its type.

**Remedy.** Delete the hand mapping and set the module's key in `.env`; an application that keeps its own spelling writes the module's key from it, `MELODY_CRON_USER=${APP_CRON_USER}`; a secret key's mark reaches the values assembled from it, not its source, so the application declares a secret source `Secret` in `ApplicationConfig.EnvironmentKeys`. A configuration double implements [`EnvironmentKeyDeclarer`](../config/contract/environment_key.go), or delegates to a `*config.Configuration`.

**Why.** A key judged only where the module reads it fails a command or a request long after the boot that could have named it, and a hand mapping repeats in every application the default and the rule the module alone knows.

### Config: `MELODY_` is the framework's and the modules' namespace

**What changed.** Every loaded key in the `MELODY_` namespace that is neither the framework's nor declared by a registered module is named, sorted, in one boot warning `application.environment.undeclaredKey` (context `environmentKeys`, names only) and marked secret. A command leaves it out under `MELODY_CLI_BOOT_WARNINGS_IGNORED=application.environment.undeclaredKey`. Such a key booted without a word and was printed in clear by `debug:parameters`.

**Symptom.** A new `WARNING` at boot naming keys, and those keys rendered `********` by `debug:parameters`: an application key spelt `MELODY_*`, a misspelt framework key (`MELODY_HTTP_ADRESS`), or the key of a module the process does not register.

**Remedy.** Rename the application's own key outside the namespace, correct the misspelling, or register the module in the process that reads its key. A key that belongs to a module registered only in another process of the same `.env` stays named; a command lists the code in `MELODY_CLI_BOOT_WARNINGS_IGNORED`.

**Why.** A misspelt key is a configuration the deployment believes it set; and a credential of a module the process does not register is a value nobody marked.

### Http: a route is registered through `Handle(pattern, handler, RouteOptions)`

**What changed.** [`RouteHandler`](../http/contract/router.go), the registration surface of a router and of a group, declares one door, `Handle(pattern, handler, RouteOptions)`, the three-argument `Handle(method, pattern, handler)` gone and `HandleWithOptions` renamed `Handle`; `RouteOptions{}` registers the route for every method, so a call shortened from the third major names its method in `Methods`; `HandleNamed`, `HandleController` and `HandleNamedController` are gone from the interface, `Router` and `RouteGroup`. [`Application.RegisterHttpRoute`](../application/application_http.go) takes the same shape, `RegisterHttpRoute(pattern, handler, RouteOptions)`, where it took `(method, pattern, handler)`, so the application's own routes carry a name, their methods and the rest of `RouteOptions` as well. [`ControllerHandler`](../http/router_utility.go) builds the handler a controller function is registered as, with the argument resolution and the shape refusals the controller doors applied, and refuses a nil controller and a nil function with a panic naming it, where the doors panicked with a reflection error on a nil and admitted a nil function that panicked at each request. [`ServerSentEventHub.ClearBackplaneWithContext`](../http/server_sent_event_hub.go) is named `ClearBackplane`; it had no context-less twin, and it behaves as before.

**Symptom.** A call of `HandleNamed`, `HandleController`, `HandleNamedController`, `HandleWithOptions`, `Handle(method, pattern, handler)`, `RegisterHttpRoute(method, pattern, handler)` or `ClearBackplaneWithContext` does not compile. A type that implements `RouteHandler` still compiles with the old doors left on it, `HandleWithOptions` included, but nothing calls them: a double or a decorator that intercepted `HandleNamed` is passed over, and the framework's `Handle` reaches what it wraps, a nil embedded router included.

**Remedy.** `router.Handle(method, pattern, handler)` is `router.Handle(pattern, handler, http.RouteOptions{Methods: []string{method}})`, `router.HandleWithOptions(…)` is `router.Handle(…)` and `router.HandleNamed(name, method, pattern, handler)` is `router.Handle(pattern, handler, http.RouteOptions{Name: name, Methods: []string{method}})`; a controller is `router.Handle(pattern, http.ControllerHandler(controller), http.RouteOptions{Methods: []string{method}})`; `app.RegisterHttpRoute(method, pattern, handler)` is `app.RegisterHttpRoute(pattern, handler, http.RouteOptions{Methods: []string{method}})`; `hub.ClearBackplaneWithContext(ctx)` is `hub.ClearBackplane(ctx)`. A double or a decorator of a router intercepts `Handle(pattern, handler, options)` where it intercepted `HandleNamed`.

### Translation: a catalog refuses a select or plural that is not keyword-and-block pairs or is nested past what Translate renders

**What changed.** [`MapCatalog.Add`](../translation/catalog.go) panics on a message whose `plural` or `select` style has a selector keyword with no block, a block with no keyword, or no `other` block, nested blocks included, and on a `plural` or `select` nested deeper than 32 levels, naming the locale, the domain, the message id, the rune offset and the reason; [`JsonDirectoryLoader.Load`](../translation/json_loader.go) returns the same refusal naming the file and the directory too, and loads no catalog. Such a message rendered without an error: a keyword with no block ended the parse and dropped every later selector, a block with no keyword was dropped, and a style with no matching branch rendered the empty string; the blocks of a selector nested past 32 levels rendered as their raw text. [`ValidatePattern`](../translation/message_format.go) is the check, exported for a catalog of the application's own; `Translate` answers such a pattern of a catalog of the application's own as its error, the empty string beside it.

**Symptom.** A boot that loads a catalog holding such a message fails, naming the message and the offset, where the third major's `Trans` answered an empty or partial string for it, the whole message with the stray keyword or block ignored, or the raw text of the blocks nested past 32 levels. A `select` or a `plural` written without `other`, relying on the empty string for an unmatched value, is refused.

**Remedy.** Give every selector its block and every style an `other` block (`other {}` for an empty answer), and nest at most 32 selectors. A catalog of the application's own runs each pattern through `translation.ValidatePattern` when it is built. A lone brace in plain text and an unknown argument type are admitted, as they render visibly.

### Storage: deleting a key that names a directory is an error

**What changed.** [`LocalStorage.Delete`](../storage/local.go) of a key that resolves to a directory refuses with [`storagecontract.ErrKeyIsDirectory`](../storage/contract/error.go) in the chain and removes nothing, where it answered `nil` and removed nothing. `Get`'s refusal of such a key carries the same cause, where it was a message alone; `Exists` still answers `false` without an error, since a directory is not an object. A symlink under the key is still removed as a link. The S3 storage has no directories and never answers it.

**Symptom.** A `Delete` of a prefix, which removed nothing and reported success, returns an error.

**Remedy.** Delete the objects under the prefix one by one; a caller that treated the prefix as removable checks `exception.ChainIs(err, storagecontract.ErrKeyIsDirectory)` on the `Delete` error where it means "nothing to delete".

### Http: two middleware definitions of one name must agree on duplicates

**What changed.** [`Builder.Build`](../http/middleware/pipeline/builder.go) and `Describe` read every definition of a name the environment and the group admit, where only the later one's flags were read. Definitions whose `AllowDuplicates` differ are refused with `middleware definitions of one name disagree on AllowDuplicates`, naming the name and the two positions, in either order; a definition setting both `ReplaceExisting` and `AllowDuplicates` is refused whatever the environment and group, where `ReplaceExisting` was ignored; a `ReplaceExisting` definition over a name admitted more than once is refused, where it replaced the first. All allowing duplicates still run side by side, and all single still keep the earliest.

**Symptom.** A pipeline where a later registrar declared a single middleware's name with `AllowDuplicates`, or the reverse, fails at the build with the refusal, where the order decided whether one or both ran.

**Remedy.** Declare every definition of the name with the same `AllowDuplicates`, give the later one a name of its own, or have it set `ReplaceExisting` alone to take the earlier one's place. A name registered once per environment or group is not compared across them.

### Http: a full limiter table evicts its least recently seen key

**What changed.** A [`FixedWindowLimiter`](../http/middleware/rate_limit.go) or `SlidingWindowLimiter` whose table holds `MaxKeys` keys always gives the slot of the key whose last request is the oldest, a denied one included, to an unseen key, which is admitted; the evicted key comes back with a full budget. The third major denied the unseen key until an idle key was pruned, unless `SetEvictOldestKeyAtCeiling(true)` was called, and that setter has no successor. The prune that walked a full table once per window is gone; the periodic sweep every `CleanupInterval` still forgets the idle keys.

**Symptom.** A limiter left at the third major's default admits an unseen key at a full table, where it answered `429`.

**Remedy.** None for most applications; drop `SetEvictOldestKeyAtCeiling(true)`. A limit that must hold against a caller who can make the table cycle — `MaxKeys` requests under other keys after its last one — raises `MaxKeys` or runs the distributed limiter of `integrations/rueidis`, which keeps no table.

### Config: a process must be told its environment, and a per-environment file cannot rename it

**What changed.** A process whose `MELODY_ENV` no `.env` artifact sets fails the boot in every mode, a command included, naming the key; an http process whose artifacts contributed no key at all is refused for that first. The boot warning `application.environment.defaulted` is gone with it, and `MELODY_CLI_BOOT_WARNINGS_IGNORED` naming it is refused as an unknown code. [`EnvironmentSource.Load`](../config/environment_source.go) refuses a `.env.<name>` or `.env.<name>.local` that leaves `MELODY_ENV` naming another environment than the one that selected it, once the references are expanded, naming the key, both names and the two files; repeating the name is admitted. It refuses an unquoted value that is not valid UTF-8 too, naming the key and the file and never the value; a quoted value is still kept byte for byte.

**Symptom.** A process started without `MELODY_ENV` in its `.env` files, an http server that booted as `dev` with the warning or a command that booted silently, exits naming the key. A layout whose `.env.prod` says `MELODY_ENV=dev` exits naming both names. A file holding a Latin-1 value without quotes exits naming its key, where the value reached its parameter with U+FFFD in place of each such byte.

**Remedy.** Set `MELODY_ENV` in `.env` or `.env.local`: a process environment variable does not set it, since melody reads configuration only from the `.env` artifacts. A command run before any `.env` exists, code generation in a build stage for instance, writes one first, `printf 'MELODY_ENV=prod\n' > .env`. Remove `MELODY_ENV` from the per-environment files, or make it repeat the name. Quote a value that is not UTF-8. Drop `application.environment.defaulted` from `MELODY_CLI_BOOT_WARNINGS_IGNORED`.

### Exception: the infrastructure mark lives in `exception`, and the error renderer answers it `503` with `Retry-After`

**What changed.** [`MarkInfrastructureFailure`](../exception/infrastructure_failure.go) and [`IsInfrastructureFailure`](../exception/infrastructure_failure.go) moved from `security` to `exception`, beside [`AnswersUnavailable`](../exception/infrastructure_failure.go), the rule the renderer and the security machinery read; `security.MarkInfrastructureFailure`, `security.IsInfrastructureFailure` and `security.ResolveUnavailableRetryAfter` are gone, the last as [`config.ResolveUnavailableRetryAfter`](../config/limit.go). [`RenderExceptionResponse`](../http/exception_listener.go) answers a marked error from any layer, an application's storage, cache or handler, `503 service unavailable` with `Retry-After`, unless an http status answers outside what the mark wraps, which is kept; a store's own status beneath the mark is answered `503` and its message, code and parameters are not served. The delay is the kernel's: [`ApplicationConfig.HttpKernel.UnavailableRetryAfter`](../application/application_new.go) or the new key `MELODY_HTTP_UNAVAILABLE_RETRY_AFTER`, the key winning, 5s when left out, 1s to 1h in whole seconds, unlimited refused, a refusal failing the boot naming the key or the field. A handler's panic whose value carries the mark is answered the same way; the security resolution listener still reads a token source's panic as a refusal. It reaches a listener as `KernelExceptionEvent.UnavailableRetryAfter()`, a `time.Duration`, and the renderer as `ExceptionRenderConfig.UnavailableRetryAfter`; the http kernel takes it through `KernelConfig.UnavailableRetryAfter`, `NewKernelExceptionEvent` takes a fourth argument, `KernelExceptionEventConfig{UnavailableRetryAfter}`, and `configcontract.HttpConfiguration` gains `UnavailableRetryAfter() bound.Duration`, the key as set. A firewall's refusal keeps the firewall's own delay.

**Symptom.** `security.MarkInfrastructureFailure`, `security.IsInfrastructureFailure` and `security.ResolveUnavailableRetryAfter` no longer compile. A handler, a storage or a cache returning, or panicking with, a marked error gets `503` with `Retry-After` where it got `500`. An own `configcontract.HttpConfiguration` no longer compiles without `UnavailableRetryAfter`, and a call of `NewKernelExceptionEvent` without its config.

**Remedy.** Spell the mark `exception.MarkInfrastructureFailure` and the reader `exception.IsInfrastructureFailure`. Set `ApplicationConfig.HttpKernel.UnavailableRetryAfter`, or the key in a deployment, for another delay than 5s. A listener that answers through `RenderExceptionResponse` passes `ExceptionRenderConfig{UnavailableRetryAfter: bound.Of(event.UnavailableRetryAfter())}`; one that builds its own response asks `exception.AnswersUnavailable(err)`, answers `503` and sets `http.SetRetryAfter(response, event.UnavailableRetryAfter())`. Pass `http.KernelExceptionEventConfig{}` to `NewKernelExceptionEvent`, or the delay an own kernel was configured with. Return an unmarked error where a `500` is the intended answer. Add `UnavailableRetryAfter() bound.Duration`, answering `bound.Duration{}`, to an own http configuration.

### Session: `Manager.Session` and `NewSession` answer an error, and a storage outage is `503` with `Retry-After`

**What changed.** [`session/contract.Manager`](../session/contract/manager.go) declares `Session(runtimeInstance containercontract.Runtime, sessionId string) (Session, error)` and `NewSession(runtimeInstance containercontract.Runtime) (Session, error)`; the runtime first is the section "Session: every operation takes the runtime first" above. The framework's [`session.Manager`](../session/manager.go) answers a storage that could not answer, on those two doors and on `SaveSession`, `DeleteSession` and `RegenerateSession`, as an error carrying [`exception.MarkInfrastructureFailure`](../exception/infrastructure_failure.go), the storage's own error in the chain, where `Session` and `NewSession` panicked; 128 minted ids in a row already held is an unmarked error. The http kernel answers a load or mint error to the session's first reader, [`http.RequestSession`](../http/request_session.go) (the next section): a handler that returns a marked one answers `503` with the kernel's `Retry-After`, any other `500`, and no session cookie is written. A firewall's sign-in door with an `Authenticator` reads the session before it and the sign-out door before its `Handler`: a session read or rotation that meets the outage refuses the door `503` with the firewall's `Retry-After`, reason `session_unavailable`, with no sign-in event.

**Symptom.** A call of `Session` or `NewSession` with one result no longer compiles, nor an out-of-tree `Manager` declaring the one-result forms. During a session storage outage a request that reads the session answers `503` with `Retry-After` and one that does not is served, where every request answered `500`. An error returned by `SaveSession`, `DeleteSession` or `RegenerateSession` for a storage failure is the framework's error wrapping the storage's, so a comparison by identity no longer matches; `errors.Is` still does.

**Remedy.** Read both results: `sessionInstance, loadErr := manager.Session(runtimeInstance, sessionId)`, then `nil != loadErr` before `nil == sessionInstance`. An out-of-tree manager returns its storage's error through `exception.MarkInfrastructureFailure` for an outage, and an unmarked error for a fault of its own. Compare storage errors with `errors.Is`.

### Http: the session is read through `http.RequestSession`, loaded at its first read

**What changed.** `http.RequestAttributeSession` is removed. [`http.RequestSession(request)`](../http/request_session.go) answers the request's session and an error, and [`http.SetRequestSession(request, session)`](../http/request_session.go) publishes one, a nil session refused. The kernel no longer loads the session before routing: the first `RequestSession` of a request loads it, or mints one when the cookie names none, once, and answers every later read the same session or the same error; a request that never reads it makes no storage call and writes no cookie. A load that failed is filed once, by the read, and every reader gets it marked logged. The response path saves the session published with `SetRequestSession`, else the one a read loaded. A firewall's sign-in door reads the session before its `Authenticator` and its `Handler`, and its sign-out door before its `Handler`, both refusing a session outage `503` with the firewall's `Retry-After`, reason `session_unavailable`, [`session.ErrSessionUnavailable`](../session/manager.go) in the chain.

**Symptom.** Code that names `http.RequestAttributeSession` no longer compiles. A route that does not read the session no longer asks the storage for it, so a session storage outage no longer reaches it, and its `Load` count drops to none. A sign-in door over a request the kernel did not serve, so carrying no session, refuses `500` before its `Authenticator` and its `Handler`, and a sign-out door refuses it `500` before its `Handler`, where it skipped the clear. A sign-in during a session storage outage is refused `503` before any credential is checked.

**Remedy.** Read the session through the door and return its error:

```go
sessionInstance, sessionErr := melodyhttp.RequestSession(request)
if nil != sessionErr {
    return nil, sessionErr
}
```

Publish a replacement with `melodyhttp.SetRequestSession(request, replacement)` and rotate with `melodyhttp.RegenerateRequestSession(request)`. A `securitycontract.TokenResolver` that reads the session answers `security.NewUnavailableToken(sessionErr)` for an error `exception.AnswersUnavailable` reads as unavailable, so a public route is served as not signed in and a protected one answers `503` with the firewall's `Retry-After`.

### Http: a session storage outage can serve an empty session, opt-in

**What changed.** [`httpcontract.SessionPolicy{EmptyOnOutage}`](../http/contract/kernel.go), installed through `KernelConfig.SessionPolicy`, and under an application through `ApplicationConfig.HttpKernel.SessionPolicy.EmptyOnOutage` or the new key `MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE`, the key winning and a value that is not a boolean failing the boot naming the key, answers a reader of the session an empty [`session.UnavailableSession`](../session/unavailable_session.go) and no error when the storage cannot answer, where the default answers the marked error. The session has no id and keeps what the request writes for the request alone: the manager refuses to save or rotate it, marked, with `session.ErrSessionUnavailable` in the chain, and the response path writes no cookie and expires none, so the client finds its session when the storage answers again. One Warning record is filed for the request. An unmarked manager error stays an error, and the firewall's sign-in and sign-out doors refuse the empty session `503` as they refuse the outage. `configcontract.HttpConfiguration` declares `SessionEmptyOnOutage`.

**Symptom.** An out-of-tree `configcontract.HttpConfiguration` no longer compiles. With the policy off, one thing changes: a sign-in door without an `Authenticator` reads the session before its `Handler`, as the door with one does, so during a session storage outage it refuses `503` before the `Handler` runs, where the `Handler` ran and met the outage at its rotation.

**Remedy.** Add the method to an out-of-tree implementation. With the policy on, a `securitycontract.TokenResolver` that reads the session answers the unavailable token for the empty session, so a protected route answers `503` rather than sending a signed-in visitor to sign in:

```go
if cause, isUnavailable := melodysession.SessionUnavailableCause(sessionInstance); true == isUnavailable {
    return melodysecurity.NewUnavailableToken(cause), nil
}
```

A handler that must not lose a write, a cart line or a consent, reads `session.SessionUnavailableCause` and returns the cause to be answered `503` with `Retry-After`. A decorator of a session forwards `SessionUnavailableCause`, since the response path and the manager read the capability on the session they are handed.

### Http: a rate limiter whose store cannot answer is `503` with `Retry-After`, not `429`

**What changed.** The rate-limit middleware and [`RateLimitGate`](../http/middleware/rate_limit_listener.go) refuse a denial whose `Allow` error is answered as unavailable, one carrying [`exception.MarkInfrastructureFailure`](../exception/infrastructure_failure.go) with no http status around the mark ([`exception.AnswersUnavailable`](../exception/infrastructure_failure.go)), with that error, which the kernel answers `503` with its `Retry-After`, and do not call `OnLimitExceeded`. The Redis limiter of `integrations/rueidis/v4` marks every store failure, under both failure modes.

**Symptom.** Under the closed failure mode a Redis outage answers `503` with `Retry-After` on every limited route where it answered `429`, a firewall's sign-in door limiter included, and an `OnLimitExceeded` handler is not called for it. A `kernel.exception` listener that answers every `429` no longer sees these.

**Remedy.** Nothing for a limiter that answers its own denial; one of an application's own returns its store failure through `exception.MarkInfrastructureFailure` beside the denial to be answered `503`, and keeps an unmarked error for a `429`. A listener that builds its own response for a marked error sets `http.SetRetryAfter(response, event.UnavailableRetryAfter())`.

### Http: one `RateLimiter` contract answers a decision, and a `429` carries `Retry-After`

**What changed.** [`RateLimiter`](../http/contract/middleware.go) is one contract: `Allow(runtimeInstance, key) (RateLimitDecision, error)` and `Reset(runtimeInstance, key) error`, both taking the request's runtime first; `RuntimeRateLimiter` and `AllowWithRuntime` are removed, and the framework probes no limiter for a widening. [`RateLimitDecision`](../http/contract/middleware.go) carries `Allowed`, already the failure policy's answer and honoured beside an error, `RetryAfter`, set on a denial the limiter can time, `Remaining` and `Limit`. A denial answers `429` with the decision's `Retry-After`: the default answer through `HttpErrorConfig.RetryAfter`, and a response `OnLimitExceeded` answers without a `Retry-After` header of its own gets the decision's. `OnLimitExceeded` is `func(request, decision) (Response, error)`. The framework's limiters refuse a nil runtime with an unmarked error beside a denial. A limiter's failure is recorded once by the middleware or the gate, at warning when the request's own context ended it.

**Symptom.** `not enough arguments in call to limiter.Allow`; `undefined: httpcontract.RuntimeRateLimiter`; `cannot use allowed (variable of type RateLimitDecision) as bool value`; `*MyLimiter does not implement RateLimiter (wrong type for method Allow)`; an `OnLimitExceeded` of one argument no longer compiles. A client over budget reads `Retry-After` on the `429`.

**Remedy.** `decision, allowErr := limiter.Allow(runtimeInstance, key)` and read `decision.Allowed`; delete the type assertion on `RuntimeRateLimiter`; `if resetErr := limiter.Reset(runtimeInstance, key); nil != resetErr { … }`. A limiter of the application's own answers the decision, `RetryAfter` zero when it cannot time a denial, and marks a store failure with `exception.MarkInfrastructureFailure`; an `OnLimitExceeded` takes the decision as its second argument.

### Session: one `StorageTimeout` bounds every storage call, and a failed save or logout answers by its cause

**What changed.** [`ManagerConfig.StorageTimeout`](../session/manager.go) and the contract's `StorageTimeout()`, with `DefaultStorageTimeout`, `StorageTimeoutRule()` and `ResolveStorageTimeout`, are the one bound of every call the kernel makes to the manager: the load of the request's session and the mint after a miss under one context, the rotation under another, both on the request's context, so a client that left stops them, and the save and the delete detached, as before. `MELODY_HTTP_SESSION_STORAGE_TIMEOUT` (`kernel.http.session_storage_timeout`, `HttpSessionStorageTimeoutKey`, `HttpConfiguration.SessionStorageTimeout`) sets it; `MELODY_HTTP_SESSION_PERSISTENCE_TIMEOUT`, the spelling earlier builds of the fourth major read, is retired and refuses the boot naming its successor. A call the bound cut is an outage whatever the manager answered, recorded naming `bound: ManagerConfig.StorageTimeout`. A failed save answers the empty `503` with `Retry-After` only when the failure answers unavailable or the bound cut it, and the empty `500` with no `Retry-After` otherwise. A logout whose delete failed answers `503` with `Retry-After` and keeps the cookie on the same terms, where the handler's response was served with the cookie expired, and `500` with the cookie expired otherwise; a committed response keeps its status. The logout door raises `security.logout.success` once the delete landed and `security.logout.failure`, with the delete's error, when it did not, where the success was raised before the delete.

**Symptom.** `*MyManager does not implement sessioncontract.Manager (missing method StorageTimeout)`. A request held by a slow storage at its first read ends after 5s with `503`. A logout over a storage outage answers `503` where it answered the handler's response, and raises `security.logout.failure` where it raised `security.logout.success`.

**Remedy.** A replaced manager or a decorator answers `StorageTimeout()` and marks its storage failures with `exception.MarkInfrastructureFailure`; a client retries the logout that answered `503`, its cookie still naming the session; a sign-out audit reads `security.logout.success` as a session ended and `security.logout.failure` as one still stored. `MELODY_HTTP_SESSION_STORAGE_TIMEOUT=unlimited` or `StorageTimeout: bound.UnlimitedDuration()` for no bound.

### Httpclient: a client is built from one configuration struct, and stays as built

**What changed.** [`NewHttpClient`](../httpclient/http_client.go) takes an [`HttpClientConfig`](../httpclient/http_client_config.go) struct, `BaseUrl`, `Timeout`, `Headers`, `Transport`, `RedirectsDisabled` and `MaxRedirects`, and answers the client with an error; the third major took the `*HttpClientConfig` that `NewHttpClientConfig(baseUrl, timeout, headers)` built, chained `WithoutRedirects` and `WithTransport` onto it, and panicked on a refused base url or header map. `Timeout` is a [`bound.Duration`](../bound/limit.go): left out it is 30s, a value is at least 1ms, and `bound.UnlimitedDuration()` is no deadline, where a non-positive timeout read as the default. `MaxRedirects`, a `bound.Count` left out at 10, bounds the requests an exchange makes while it follows redirects, the first included as net/http counts its own cap, and is refused beside `RedirectsDisabled`. `HttpClient.SetBaseUrl` and `HttpClient.SetTimeout` are removed, and so are `DefaultTransportConfig`,
`TransportDuration` and `TransportCount`.

**Symptom.** A call of `NewHttpClientConfig`, of `SetBaseUrl` or of `SetTimeout`, and a call of `NewHttpClient` that reads one return value, no longer compile.

**Remedy.** `httpclient.NewHttpClient(httpclient.HttpClientConfig{BaseUrl: "https://host/v1/", Timeout: bound.Of(5 * time.Second), Headers: headers, RedirectsDisabled: true})`, and handle the error; a container factory returns it as it is. A caller that rewired a live client builds another client with the new base url or timeout.

### Httpclient: a request takes one options struct

**What changed.** `Get`, `Delete` and `Request` take the runtime first and a [`RequestOptions`](../httpclient/contract/request_option.go) struct in place of the variadic `RequestOption` functions, and `Post`, `Put` and `Patch` take the runtime first and the struct after their positional body: `Headers`, `Query`, `Body`, `Json`, `Timeout`, `BearerToken`, `BasicAuth` and `MaxResponseBodyBytes`. `NewRequestOptions`, the ten `With*` functions, the `RequestOption` type and the `AuthorizationOptions` and `BasicAuthorizationOptions` types and contracts are removed. A `Body` and a `Json` together, either beside the positional body of `Post`, `Put` or `Patch`, and a `BearerToken` beside a `BasicAuth` are refused before anything is dialled, where the last option or the bearer won. `Timeout` left out is the client's, a value replaces it and unlimited is no deadline; `MaxResponseBodyBytes` left out is 10 MiB, a value is at least one byte, and unlimited is refused. The runtime's context ends the exchange
when it is cancelled, the body read included, and an absent runtime is refused with `the runtime is nil or carries no context`; the third major ran every buffered request under `context.Background()`.

**Symptom.** A call that passes `With*` options, or none, or a context where the runtime goes, no longer compiles. A request that set both a bearer token and a basic credential, or a body twice, answers an error instead of being sent.

**Remedy.** Bind the client to the runtime and fold the options into one literal: `httpclient.For(runtimeInstance, client).Get("users", httpclient.RequestOptions{Headers: map[string]string{"X-Trace": id}, Query: map[string]string{"page": "2"}, Timeout: bound.Of(2 * time.Second), BearerToken: token})`; a call without options passes `httpclient.RequestOptions{}`. Several `WithHeader` calls become one `Headers` map, `WithBasicAuth(user, password)` becomes `BasicAuth: &httpclient.BasicAuth{Username: user, Password: password}`, and `WithJson(data)` on `Request` becomes `Json: data`. Keep one credential and one body per request. A client the application registered under its own name binds with `httpclient.FromRuntime(runtimeInstance, name)`; the framework registers none. Where no runtime is at hand, a run over `context.Background()` is the third major's behaviour. A client of the application's own implementing `httpclientcontract.Client` declares the runtime first and builds its request over `runtimeInstance.Context()`; the wiring generator refuses a constructor argument of type `httpclient.Bound`.

### Httpclient: `RequestStream` takes the runtime first

**What changed.** [`RequestStream`](../httpclient/http_client.go) takes the runtime as its first parameter, the stream bound to its context, and its options as one struct; `RequestStreamWithContext`, the form the third major named so, is removed. A `Timeout` left out or unlimited leaves the stream without a whole-stream deadline and a value bounds the whole stream, where a negative timeout bound the stream to the client's own timeout.

**Symptom.** A call of `RequestStream(method, url, ...)` or of `RequestStreamWithContext` no longer compiles.

**Remedy.** `httpclient.For(runtimeInstance, client).RequestStream(method, url, httpclient.RequestOptions{})`, binding a run over `context.Background()` where no context was bound. A stream that was bound to the client's timeout through a negative timeout sets that value as its `Timeout`.

### Application: an application is built from one configuration struct, and the server limits are configured

**What changed.** [`NewApplication`](../application/application_new.go) takes the context and an [`ApplicationConfig`](../application/application_new.go) struct, `EnvironmentFiles`, `PublicFiles`, `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` and `TeardownTimeout`, in place of the two embedded filesystems positionally. The five server limits the third major fixed at 15s, 5s, 30s, 60s and 1 MiB are now configured, by the field and by the new keys `MELODY_HTTP_READ_TIMEOUT`, `MELODY_HTTP_READ_HEADER_TIMEOUT`, `MELODY_HTTP_WRITE_TIMEOUT`, `MELODY_HTTP_IDLE_TIMEOUT` and `MELODY_HTTP_MAX_HEADER_BYTES`, and the teardown budget by the field and `MELODY_TEARDOWN_TIMEOUT`. A key the deployment sets wins over the field, the field over the framework default. A field left out takes the third major's value. A value below 1ms, 4 KiB on the header cap, is refused naming the field, and unlimited is refused on the five server limits and admitted on the teardown budget.

**Symptom.** `application.NewApplication(ctx, environmentFiles, publicFiles)` no longer compiles. A field or a key below its minimum fails the boot, naming the field.

**Remedy.** `application.NewApplication(ctx, application.ApplicationConfig{EnvironmentFiles: environmentFiles, PublicFiles: publicFiles})`; a deployment that wants other limits sets the keys, an application that wants other defaults for its deployments sets the fields.

### Application: the runtime flags are built and parsed from one struct

**What changed.** [`NewRuntimeFlags`](../application/cli.go) and [`ParseRuntimeFlags`](../application/cli.go) take a [`RuntimeFlagsConfig`](../application/cli.go) struct, `Mode` and `Role`, the empty role reading as `all`. `NewRuntimeFlagsWithRole` and `ParseRuntimeFlagsWithRole`, the siblings the third major kept beside the one-parameter forms, are removed.

**Symptom.** A call of `NewRuntimeFlags("http")`, `ParseRuntimeFlags("http")` or of either `WithRole` form no longer compiles.

**Remedy.** `application.ParseRuntimeFlags(application.RuntimeFlagsConfig{Mode: config.ModeHttp, Role: config.RoleWorker})`.

### Config: the limit keys are bound limits, the session ttl defaults to 24h, and unlimited is spelled

**What changed.** `MELODY_HTTP_MAX_REQUEST_BODY_BYTES`, `MELODY_STATIC_CACHE_MAX_AGE`, `MELODY_HTTP_SESSION_TTL`, `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION`, `MELODY_HTTP_SHUTDOWN_TIMEOUT` and `MELODY_TEARDOWN_TIMEOUT` are read as [`bound`](../bound/limit.go) limits: the empty value takes the default, `unlimited` lifts the bound where the field admits it, and a value below the field's minimum is refused naming the key and the field. The minimums are one byte, 0, 1s, 1s, 1ms and 1ms, and only the session ttl and the teardown budget admit `unlimited`. [`HttpConfiguration`](../config/contract/http.go) answers the five http limits as a `bound.Resolved`, and declares `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout` and `MaxHeaderBytes`, the server keys as the deployment set them; [`KernelConfiguration`](../config/contract/kernel.go) declares `TeardownTimeout`, the teardown key as the deployment set it. The session ttl defaults to 24h, where the third major defaulted to no expiry, and no expiry is written `unlimited`: a `0` is refused, as it is on the teardown budget, where the third major read it as no deadline. `config.MinimumSessionTtl` is removed, the minimum is the rule's. [`NewEnvironmentSource`](../config/environment_source.go) takes an `EnvironmentSourceConfig`, `FileSystem` and `BaseDirectory`, the empty base directory reading as the filesystem's root.

**Symptom.** A deployment that wrote `MELODY_HTTP_SESSION_TTL=0` or `MELODY_TEARDOWN_TIMEOUT=0` fails the boot. A deployment that left the session ttl out now ends a session 24 hours after its last write, where it kept it forever. `configuration.Http().SessionTtl()` and the other four http limits are a `bound.Resolved`, so a comparison against a duration or an int no longer compiles, and a type of your own implementing either contract no longer compiles until it declares the new methods.

**Remedy.** Write `unlimited` where the deployment meant no expiry or no deadline, and set the session ttl to the lifetime the deployment wants; read `.Value` and `.Unlimited` of a resolved limit. A test double answers `bound.Resolved[int]{Value: ...}` or `bound.Resolved[time.Duration]{Value: ...}` for the five http limits and an absent `bound.Duration{}` or `bound.Count{}` for the six application limits. `config.NewEnvironmentSource(config.EnvironmentSourceConfig{FileSystem: fileSystem})`.

### Cli: the output option's zero value is the default, its counts are bound limits, and the withdrawn flags are gone

**What changed.** The zero [`output.Option`](../cli/output/option.go) is the default, a table in ascending order, so `output.DefaultOption` is removed. `Limit`, `Offset` and `TableMaxWidth` are `bound.Count`: the limit left out is no limit and `0` is the value zero, the offset left out is 0, the table width left out is 120 and unlimited lets the table be as wide as its content; a negative value is refused naming the field, where it was clamped to zero. [`NormalizeOption`](../cli/output/option_parser.go), [`SelectPrinter`](../cli/output/printer_selector.go) and [`WindowItems`](../cli/output/list_payload.go) answer that refusal as an error, and [`NewListPayload`](../cli/output/list_payload.go) and the envelope carry the limit and the offset as given, `null` when left out. The `--table-width` flag defaults to 120 and refuses 0. The deprecated `Option.Fields` and `Option.SortKey`, the `--fields` and `--sort` flags, `FlagNameFields`, `FlagNameSortKey`, `SplitFields` and the envelope's `fields` and `sortKey` keys are removed, as the third major's deprecation notices announced.

**Symptom.** `output.DefaultOption`, `--fields` and `--sort` are unknown; a call of `NormalizeOption` that reads one return value, or that passes an int as a count, no longer compiles; `--limit=0` lists nothing where it listed everything, and `--table-width=0` is refused where it read as the default width; a json consumer finds `"limit": null` where it found `0`.

**Remedy.** `output.Option{}` in place of `output.DefaultOption()`; `Limit: bound.Of(20)`; handle the error `NormalizeOption` answers, returning it from the command; leave `--limit` out for no limit.

### Http: the kernel is built from one configuration struct whose zero value is the default

**What changed.** [`NewKernel`](../http/kernel.go) takes the router and a [`KernelConfig`](../http/kernel.go) struct, `MethodPolicy`, `ForwardedHeadersPolicy`, `SessionCookiePolicy`, `Middlewares` and `NotFoundHandler`, whose zero value is the framework's default: HEAD falls back to GET and an unrouted OPTIONS is answered, no forwarded header is believed, the session cookie is sent for `/` with SameSite=Lax, no middleware, the framework's not-found and error rendering. [`MethodPolicy`](../http/contract/kernel.go) reads `HeadFallbackDisabled` and `AutomaticOptionsDisabled`, so its zero value keeps both halves on, where the third major's `HeadFallbackToGet` and `AutomaticOptions` read the zero value as both off and a kernel started from `DefaultKernelOptions`. `KernelOptions` and `DefaultKernelOptions` are removed. The boot-time setters stay as the door a module installs through, and `Kernel.Use` takes the middlewares as a slice.

**Symptom.** `http.NewKernel(router)`, `http.DefaultKernelOptions()`, `MethodPolicy{HeadFallbackToGet: true}` and `kernel.Use(first, second)` no longer compile.

**Remedy.** `http.NewKernel(router, http.KernelConfig{})`; an api that answers `405` to OPTIONS writes `MethodPolicy: httpcontract.MethodPolicy{AutomaticOptionsDisabled: true}` in the struct; `http.KernelConfig{Middlewares: []httpcontract.Middleware{first, second}}` on a bare kernel, `Application.RegisterHttpMiddlewares` in an application.

### Http: a session cookie policy of `SameSite=None` with `SessionCookieSecureNever` is refused

**What changed.** [`NewKernel`](../http/kernel.go), and so `NewApplication` over `ApplicationConfig.HttpKernel`, panics on a policy whose `SameSite` is `None` and whose `Secure` is `SessionCookieSecureNever`, naming `SessionCookiePolicy.Secure`: every current browser drops such a cookie, so no session stuck. Either half alone is admitted. The third major wrote the cookie as asked and named the pair in the `http.sessionCookie.sameSiteNoneNeverSecure` boot warning at the kernel's first request, which is removed.

**Symptom.** The boot panics with "the session cookie policy sets SameSite=None with Secure never".

**Remedy.** Leave `Secure` at `SessionCookieSecureFromScheme`, which writes a `None` cookie `Secure` whatever the scheme, or pick `Lax` for a same-site deployment without TLS.

### Http: a route group is built whole from a `GroupConfig`

**What changed.** [`Router.Group`](../http/router_group.go) and [`NewRouteGroup`](../http/router_group.go) take a [`GroupConfig`](../http/contract/router_group.go) struct, `PathPrefix`, `NamePrefix`, `Requirements` and `Defaults`, the two maps copied, and a field left out adds nothing. A group takes no setting once built: `WithNamePrefix`, `WithRequirements` and `WithDefaults`, void setters under a builder's name, are removed from `RouteGroup`.

**Symptom.** `router.Group("/api")` and a call of `WithNamePrefix`, `WithRequirements` or `WithDefaults` no longer compile.

**Remedy.** `router.Group(httpcontract.GroupConfig{PathPrefix: "/api", NamePrefix: "api."})`.

### Http: the static file server is built from one `Config` struct, and a negative max age is refused

**What changed.** [`static.NewFileServer`](../http/static/file_server.go) and [`middleware.StaticMiddleware`](../http/middleware/static.go) take one [`static.Config`](../http/static/option.go) struct, `Mode`, `PublicDirectory`, `IndexFile`, `StripPrefix`, `CacheDisabled`, `CacheMaxAge`, `WeakEtag`, `AllowedDotPrefixList`, `ExcludedPathList`, `Root` and `FileSystem`, whose zero value serves `public` under `.` with `index.html`, an hour's max age and `.well-known` the one dot-prefixed element retrieved. The third major built a `FileServerConfig` of seven positional parameters, set its two lists after through `SetAllowedDotPrefixList` and `SetExcludedPathList`, and wrapped it in `Options` with the root and the filesystem; all of them are removed. `CacheMaxAge` is a [`bound.Count`](../bound/limit.go): left out it is 3600, a value is at least 0, which is always revalidate, and unlimited is refused, as is a max age beside `CacheDisabled`, each naming `Config.CacheMaxAge`; a negative value read as the
default. `AllowedDotPrefixList` left nil is `[".well-known"]` and an empty list refuses every dot-prefixed path. The built-in server reads `MELODY_STATIC_CACHE_DISABLED` and `MELODY_STATIC_CACHE_MAX_AGE` (see [Static files: `MELODY_STATIC_CACHE_DISABLED`](#static-files-melody_static_cache_disabled-replaces-melody_static_enable_cache)).

**Symptom.** `static.NewFileServerConfig`, `static.NewOptions` and the two setters no longer compile, and `StaticMiddleware` takes a `static.Config`. A negative `CacheMaxAge` panics at construction where it served an hour.

**Remedy.** The struct literal, `static.Config{PublicDirectory: "public", AllowedDotPrefixList: []string{".well-known", ".assets"}}`; `CacheDisabled: true` for the third major's `enableCache` false; leave `CacheMaxAge` out for the default.

### Cron: the module reads `MELODY_CRON_*`

**What changed.** The [cron module](../../integrations/cron/v4/README.md#keys) declares its ten keys and no longer registers parameters: `ModuleConfig.WithDefaultParameters`, `RegisterDefaultParameters` and `ParameterRegistrar` are gone, and every `melody.cron.*` parameter exists, fed by its key or taking `ModuleConfig.KeyDefaults` or the module's default. The heartbeat switch, the k8s namespace and the restart policy are judged at boot, naming the key and the `cron.KeyDefaults` field, where they were judged only when `melody:cron:generate` ran.

**Symptom.** A boot whose application maps its own keys onto `melody.cron.*` is refused with `the parameter a declared environment key feeds is already registered`, or reports the mapping as a duplicate when a parameter hook registers it; a value the generator refused at its run now fails every process that registers the module. A module registered without `WithDefaultParameters` now writes the crontab to `generated_conf/cron/crontab` and redirects under `var/log/cron` (created on every run) when no `--out` or `--logs-directory` is passed, where both runs were refused.

**Remedy.** Delete the hand registrations and rename the lines:

| Before (the example's spelling)   | After                            |
|-----------------------------------|----------------------------------|
| `APP_CRON_USER`                   | `MELODY_CRON_USER`               |
| `APP_CRON_HEARTBEAT_AUTO_ENABLED` | `MELODY_CRON_HEARTBEAT_ENABLED`  |
| `APP_CRON_IMAGE`                  | `MELODY_CRON_K8S_IMAGE`          |
| `APP_CRON_NAMESPACE`              | `MELODY_CRON_K8S_NAMESPACE`      |
| `APP_CRON_RESTART_POLICY`         | `MELODY_CRON_K8S_RESTART_POLICY` |

An application of another spelling maps it the same way, or keeps its line and writes `MELODY_CRON_USER=${APP_CRON_USER}` in `.env`. An application that wants the refusal without `--out` or `--logs-directory` back writes the key's line empty (`MELODY_CRON_DESTINATION_FILE=`, `MELODY_CRON_LOGS_DIRECTORY=`).

**Why.** The defaults and the rules of the keys are the module's; repeating them in every application let a misspelt opt-in boot every process clean and fail at the generation.

### Amqp: the module registers no parameters

**What changed.** `ParameterDsn`, `ParameterExchange`, `ParameterPrefetch`, `ParameterRegistrar`, `RegisterDefaultParameters` and `ModuleConfig.WithDefaultParameters` are removed, and the module no longer implements `ParameterModule`.

**Symptom.** A build that names one of them fails to compile.

**Remedy.** The application reads its own key for the dsn and marks it secret, as the example does with `AMQP_DSN`, or registers a parameter of its own; a transport's prefetch is `TransportConfig.Prefetch`.

**Why.** Nothing of the module read the three parameters, and the default dsn was a credential registered in clear.

### Http: the in-process rate limiters carry no `Close`

**What changed.** `FixedWindowLimiter` and `SlidingWindowLimiter` lose their `Close`, which answered nil and held nothing. The container's teardown refuses a service carrying `Close` without `CloseWithContext`, so the limiters could not be registered as services.

**Symptom.** A call to `limiter.Close()` no longer compiles.

**Remedy.** Delete the call; a limiter holds nothing to close.

### Container: a registration declares its options in one struct, and a closer is a value

**What changed.** `Register`, `MustRegister`, `RegisterScoped` and `MustRegisterScoped` on the container, a scope and the application, the application's `RegisterService` and `RegisterScopedService`, and the generic [`Register`, `MustRegister`, `RegisterType`, `MustRegisterType`](../container/container_register.go), [`RegisterScoped`, `MustRegisterScoped`, `RegisterScopedType` and `MustRegisterScopedType`](../container/container_register_scoped.go) take one [`RegisterOptions`](../container/contract/registrar.go) struct last, `TypeRegistrationDisabled`, `TypeRegistrationRelaxed`, `CollectionPriority`, `ReplacesContainerService`, `TeardownDependencyNames`, `TeardownDependencyTypes`, `TeardownReflectionDisabled` and `Closer`, whose zero value registers the service strictly under its name and its type; `container.RegisterOptions` names the same type. The third major took variadic `RegisterOption` functions over `AlsoRegisterType` and `TypeRegistrationIsStrict`, both true by default, which the two first fields invert. A closer is a [`Closer`](../container/contract/registrar.go) value built with [`container.NewCloser[T]`](../container/register_option.go), and one built by hand with one of its two fields left out is refused naming `RegisterOptions.Closer`. `RegisterType` and `RegisterScopedType` forward the options as given, where they prepended `WithTypeRegistration(true)`, which the zero value already says. The generated wiring writes the struct on every registration.

**Symptom.** A registration without options, `registrar.RegisterService(name, provider)` or `container.MustRegister(registrar, name, provider)`, no longer compiles, nor does any `With*` option, `Replacing()` or a registrar double declaring `options ...RegisterOption`; a `wiring_gen.go` generated by an earlier build no longer compiles.

**Remedy.** Pass the zero struct, `registrar.RegisterService(name, provider, containercontract.RegisterOptions{})`, and spell each option as its field: `WithoutTypeRegistration()` is `RegisterOptions{TypeRegistrationDisabled: true}`, `WithTypeRegistration(false)` is `{TypeRegistrationRelaxed: true}`, `WithCollectionPriority(p)` is `{CollectionPriority: p}`, `Replacing()` is `{ReplacesContainerService: true}`, `WithTeardownDependency("a", "b")` is `{TeardownDependencyNames: []string{"a", "b"}}`, `WithTeardownDependencyOfType[T]()` is `{TeardownDependencyTypes: []reflect.Type{reflect.TypeFor[T]()}}`, `WithoutTeardownReflection()` is `{TeardownReflectionDisabled: true}` and `WithCloser[T](fn)` is `{Closer: container.NewCloser[T](fn)}`; several options are one literal. Generate the wiring again.

### Container: an override declares its options in one struct, on every override door

**What changed.** [`OverrideInstance`, `MustOverrideInstance`, `OverrideProtectedInstance` and `MustOverrideProtectedInstance`](../container/contract/override.go) take one [`OverrideOptions`](../container/contract/override.go) struct last, on the container and on a scope alike, whose zero value is the default: a scope leaves the value with whoever installed it, and the root container closes the value still installed at its own teardown, as it did. `ClosedWithScope` is its one field, and `container.OverrideOptions` names the same type. The third major kept the option on a separate `OverrideServiceWithOptions` contract, whose four `WithOptions` doors a scope alone implemented and a caller reached through a type assertion; that contract is folded into `OverrideService`. The root container refuses `ClosedWithScope: true` naming `OverrideOptions.ClosedWithScope`, before anything is written, since what it installs it closes at its own teardown already.

**Symptom.** `scope.MustOverrideProtectedInstance(name, value)`, `OverrideInstanceWithOptions`, `ClosedWithScope()`, the assertion on `OverrideServiceWithOptions` and a double implementing the four doors with two parameters no longer compile.

**Remedy.** Pass the zero struct, `scope.MustOverrideProtectedInstance(name, value, containercontract.OverrideOptions{})`, and `containercontract.OverrideOptions{ClosedWithScope: true}` where the option was declared, on the scope; drop the assertion, a scope is an `OverrideService`.

### Container: `DeclareTeardownDependency` takes the names as a slice

**What changed.** The concrete container's [`DeclareTeardownDependency`](../container/container_close.go) takes the dependency names as a `[]string`, the form of `RegisterOptions.TeardownDependencyNames`, nil declaring nothing; the third major took them variadic.

**Symptom.** `DeclareTeardownDependency("app.bus", "app.transports")` and an interface asserting the variadic form no longer compile.

**Remedy.** `DeclareTeardownDependency("app.bus", []string{"app.transports"})`, and declare the slice in the asserted interface.

### Http: a route attribute beginning with an underscore is refused at registration

**What changed.** [`RouteOptions.Attributes`](../http/contract/route_option.go) refuses a key beginning with `_`, the prefix of the framework's route and request attributes, naming `RouteOptions.Attributes` and the key. The third major copied every key, so a route could publish a spoofed `_route`.

**Symptom.** Registering such a route panics with `route attribute key is reserved by the framework`.

**Remedy.** Rename the key. A route name, a host, a method or a locale goes in its own `RouteOptions` field.

### Http: the compression middleware leaves an authenticated response uncompressed

**What changed.** [`CompressionMiddleware`](../http/middleware/compression.go) sends the answer to a request carrying the session cookie or an `Authorization` header as it is, unless [`CompressionConfig.CompressAuthenticatedResponses`](../http/middleware/compression.go) is set: the length of a compressed body beside a per-user secret and reflected input leaks that secret (BREACH). The third major compressed every eligible response.

**Symptom.** An authenticated response no longer carries `Content-Encoding: gzip`, and takes more bytes on the wire.

**Remedy.** An application whose authenticated pages reflect no input beside a secret sets `CompressAuthenticatedResponses: true`. The decision reads the session cookie, so any session holder counts, logged in or not. A token source reading another cookie or header, `HmacTokenSource` and `ImpersonationTokenSource` among them, is not seen, and excludes its paths through `ExcludedPaths`. `Vary` still names `Accept-Encoding` alone, so a shared cache must not store an authenticated response.

### Security: a path prefix matcher with more than one trailing slash is refused

**What changed.** [`NewPathPrefixMatcher`](../security/matcher.go) panics on `/admin//`, naming `NewPathPrefixMatcher.prefix`. The form was admitted, and selected `/admin` alone, so `/admin/users` ran without that firewall's token source and rules.

**Symptom.** The constructor panics at boot on such a prefix.

**Remedy.** `/admin/` or `/admin`, as the reach you meant.

### Integrations: a module asked for a role it cannot serve, or handed a value it would drop, is refused at boot

**What changed.** The rueidis, bunorm mysql, bunorm pgsql and aws s3 modules panic at `RegisterServices`, naming the field, on a role with no dependency (a rueidis `Locker` or `TokenStore` with no `Connection`, a bunorm `Locker` with no database, a `Bucket` or a `Storage` with no client). The third major registered nothing, and the service was missed at its first resolve. An empty `ModuleConfig{}` still registers nothing.

The same holds for a value a module was handed and would drop, refused at the hook that would have dropped it: the amqp module's nil entry of its `Transports`, named by its transport; the outbox module's `RelayCommand` with no `Relay`; and the opentelemetry module's half-declared metrics route, a `MetricsPath` with no `Prometheus`, the reverse, or a `MetricsRouteName` with neither, refused at `RegisterHttpRoutes`. The application refuses, while it collects the modules' decorators, a nil http handler decorator a module answers, naming the module and the index, where it skipped it.

**Symptom.** A boot that passed now panics naming `ModuleConfig.Locker` or its twins, one of `ModuleConfig.CloseTimeout`, `ModuleConfig.Transports`, `ModuleConfig.RelayCommand`, `ModuleConfig.MetricsPath`, `ModuleConfig.Prometheus` and `ModuleConfig.MetricsRouteName`, or "a module answered a nil http handler decorator".

**Remedy.** Hand the dependency, or leave the module out when there is none, as `if nil != client { app.RegisterModule(...) }`; drop a value the module cannot read; check the constructor error behind a nil transport or decorator.

### Storage, translation, cors, static: four constructors refuse what they admitted

**What changed.** Four constructors refuse at construction a value the third major admitted and then dropped or failed on later. `storage.NewLocalStorage` creates and opens its base directory, refusing one it cannot create or open, where the first `Put` failed; each operation still reopens the directory. `translation.NewManager`, handed at least one catalog, refuses an empty default locale and a default or fallback locale no catalog carries, since the lookup asks them by that name (in BCP 47 case); `NewManager(nil, …)` keeps answering raw ids. `cors.NewService` refuses a blank `AllowOrigins` entry, an `AllowOrigins` list beside `AllowOrigin`, and `"*"` beside other entries. In a filesystem build, the application refuses at `New`, in every process role, an explicitly set `MELODY_PUBLIC_DIRECTORY` that is not an existing directory, resolved against the project directory as the file server resolves it, a blank value reading as `public`; the default `public` may be absent.

**Symptom.** A construction that passed now panics: "could not create the storage base directory" or "could not open the storage base directory" (`baseDirectory`); "translation default locale is empty", "translation default locale is carried by no catalog" (`defaultLocale`), "translation fallback locale is empty" or "translation fallback locale is carried by no catalog" (`fallbackLocale`, `index`); a cors misconfiguration naming `Config.AllowOrigins` and the `index`, or both fields; "the public directory does not exist", "the public directory cannot be read" or "the public directory is a file" (`MELODY_PUBLIC_DIRECTORY`), in every process role, the cli role included.

**Remedy.** Point the storage at a directory the process can create and open; hand a catalog for every default and fallback locale, or drop the locale; drop the blank entry, keep `AllowOrigin` or the list, and spell "allow everything" as `[]string{"*"}` alone; create the public directory, or unset `MELODY_PUBLIC_DIRECTORY` when the application serves no assets.

### Security, static: three boot warnings become refusals, and a protected rule needs a decision manager

**What changed.** `BuildAndCompile` refuses what the third major named in a boot warning: a prefix firewall an earlier one claims whole, which the first matching firewall never let run, and a rule path or a firewall prefix holding a percent escape other than `%2F`, which claims only a resource whose name holds the percent sign. It also refuses a firewall whose access control holds a rule that is not `PUBLIC_ACCESS` while no decision manager reaches it, neither its own nor the global one; every request that rule matched answered 500. A `MELODY_STATIC_EXCLUDED_PATHS` entry holding such an escape is refused when the configuration resolves, in every mode. The codes `security.firewallShadowed`, `security.pathPercentEncoded` and `http.static.excludedPathPercentEncoded` are gone.

**Symptom.** The boot panics with "security firewall is never selected" (`firewallName`, `shadowedBy`), "security declared path carries a percent escape" (`field`, `entry`), "security firewall access control holds a rule that is not PUBLIC_ACCESS and no access decision manager reaches the firewall" (`firewallName`, `ruleIndex`, `rule`), or "static excluded path carries a percent escape" (`MELODY_STATIC_EXCLUDED_PATHS`, `index`).

**Remedy.** Declare the narrower firewall first or bound the earlier one with `security.NewSegmentPrefixMatcher`; write the path decoded, `/café` for `/caf%C3%A9`, keeping `%2F` for a separator inside a segment; declare `GlobalConfig.AccessDecisionManager` or `FirewallConfig.Override.AccessDecisionManager`, or mark the rule `PUBLIC_ACCESS` when it protects nothing.

### Application, config: a command writes the boot warnings, two cli keys filter them, and the fallback session storage takes a configured count

**What changed.** A command writes the boot warnings the boot collected once, before it runs, into its configured journal, stderr when `MELODY_LOG_PATH` is empty, where only an http process wrote them; today `security.firewallDoorReachUnknown`, `application.environment.processVariableIgnored` and `application.secretMark.unmatched` are the framework's codes a command can write, the last two uncoded lines the third major wrote during `Boot`, now written with the others when the http server or the command starts, or before the exit record of a process that dies first, and a module declares codes of its own through `applicationcontract.BootWarningModule`, the aws/s3 module `aws.s3.plaintextEndpoint`. `MELODY_CLI_BOOT_WARNINGS_IGNORED` lists codes a command leaves out, and `MELODY_CLI_BOOT_WARNINGS_DISABLED=true` writes none. Both are read by a command only. `MELODY_HTTP_SESSION_MAX_SESSIONS`, 100,000 when left out, unlimited admitted, is the count of the framework's fallback in-memory session storage. On that storage, an unlimited `MELODY_HTTP_SESSION_TTL` beside an unlimited count is refused at the http boot. Beside a bounded count it is the boot warning `http.session.unboundedTtlOnBoundedDefaultStorage`, where the third major wrote an uncoded line saying the storage grew without end. `configcontract.HttpConfiguration` declares `SessionMaxSessions()`, and `configcontract.CliConfiguration` declares `BootWarningsIgnored()` and `BootWarningsDisabled()`.

**Symptom.** A command's journal, its stderr when `MELODY_LOG_PATH` is empty, carries the warning lines before its output. The configuration fails on "cli boot warning code may not be empty", "is listed twice" (`MELODY_CLI_BOOT_WARNINGS_IGNORED`, `index`, `code`), or on a list beside `DISABLED=true`; the boot panics on "names no boot warning the framework or a module writes" and "names a boot warning only an http process writes", with the same context and `cliCodes`. An http process panics with "a cli boot warning key is set in an http process" (`environmentKey`), or on the unlimited session pair. A configuration of the application's own implementing either contract no longer compiles.

**Remedy.** List the codes a command should leave out, or disable the cli journal of boot warnings, in the command's environment only; a module's code is accepted only while that module is registered. Bound the session ttl or the count, or register `session.storage` with a storage of your own. Add the three methods to your own configuration type.

### Container: a Lazy handle naming no service fails the boot

**What changed.** A [`container.Lazy`](../container/lazy.go) or `LazyByType` handle built over the container, or over the resolver a container provider was handed, is recorded where it is built. The application checks the record at the end of `Boot` through `VerifyLazyNames`, so a handle whose service no registration answers fails the boot where it failed at its first use, a cron tick or a request. [`lock.NewLazyLocker`](../lock/lazy_locker.go) and the outbox `NewLazyRepository` build such handles. A handle built after the check, over a scope, or over a resolver of your own is not verified. See [the container](package/CONTAINER.md#scope-teardown).

**Symptom.** The boot panics with "a container.Lazy handle names no container service". Its cause, in the journal, is "a container.Lazy handle names a service nothing registered in the container" (`serviceName`, `site`, `caller`), "a container.LazyByType handle names a type nothing registered in the container" (`type`, `site`, `caller`) "a container.LazyByType handle names a type more than one service is registered under" (`type`, `conflicts`, `site`, `caller`), or "a container.Lazy handle asserts a type its service is not" (`serviceName`, `serviceType`, `handleType`, `site`, `caller`).

**Remedy.** Register the service the handle names, `service.lock.locker` for a lazy locker, or fix the name. For a type several services stand for, build the handle by name. Give the handle the type the provider declares, `*Mailer` for a provider of `*Mailer`. For a service that is optional, build the handle only after `Has` answers true.

### Container: a Lazy handle is used with the runtime

**What changed.** [`LazyService.Get` and `Resolve`](../container/lazy.go) take the caller's `containercontract.Runtime` in place of a context. The handle still resolves through the resolver it was built over; the runtime lends its context and, where that resolver reads the container — a handle over the container, or over a container provider's resolver once the provider returned — its kind, so a service first built through a handle is created under the caller's `KindRequest`, `KindRun` or `KindBoot` where it was created detached. A scoped handle keeps its scope's kind and, until it memoized, refuses a runtime of another scope, a child run's included. An unmemoized call under a runtime whose context is done is refused before it resolves. See [the container](package/CONTAINER.md).

**Symptom.** `cannot use ctx (variable of type context.Context) as containercontract.Runtime value in argument to handle.Resolve`, the same for `runtimeInstance.Context()`. At run time "the runtime is nil or carries no context" for a nil runtime, "the runtime belongs to another scope than the lazy handle" (`handleScopeKind`, `runtimeScopeKind`) for a request's handle used under another scope's runtime, "the runtime's context is done in lazy resolve" for a done context, with `ErrBootFinished` in its cause chain under the boot runtime after `Boot`. A provider created through a handle reads `KindRequest` or `KindRun` where it read `KindDetached`, so a door it calls that refuses by kind is judged by its first caller's phase.

**Remedy.** Hand the handle the runtime the call received: `handle.Resolve(runtimeInstance)`. Inside the holder's own provider, `handle.Get(resolver.Runtime())`. Where only a context is held — a goroutine a provider started over `ForGoroutine(resolver)`, a door that takes a `ctx` — build the detached runtime over it: `handle.Get(runtime.WithContext(resolver.Runtime(), ctx))`. In a test, `runtime.NewRun(ctx, scope)` or `runtime.NewRequest(ctx, scope)`.

### Logging: the reopenable file writer can be a container service

**What changed.** [`ReopenableFileWriter`](../logging/reopenable_file_writer.go) carries `CloseWithContext` beside `Close`, so the teardown no longer refuses it as a service, and a close stuck on the watcher ends at the teardown's deadline.

**Symptom.** None; registering it no longer fails.

**Remedy.** None needed.

### Lock: a leader gate or an exclusive command with an unlimited ttl over a lease locker is refused

**What changed.** [`NewLeaderGate`](../lock/leader_gate.go) and [`NewExclusiveCommand`](../lock/exclusive_command.go) panic on an unlimited ttl when the locker's `CreateLock` refuses it, the in-memory and rueidis lockers among them. A locker they cannot read, `NewLazyLocker` included, makes the gate's `Run` report the refusal once, end at once and return it, with `lockcontract.ErrUnlimitedTtlRefused` as its cause, where the gate retried it as a store outage for the life of the process. In the third major the in-memory locker admitted the same configuration as a session-style lock, so the gate led there.

**Symptom.** `NewLeaderGate` panics with `an unlimited leader gate ttl needs a session-style locker`, `NewExclusiveCommand` with `an unlimited exclusive command ttl needs a session-style locker`, or `Run` returns `a lease lock does not admit an unlimited ttl`.

**Remedy.** A lease ttl, or a session-style locker (the mysql or pgsql one). A lease locker of your own refuses an unlimited ttl from its `CreateLock` (see the section on `CreateLock` answering an error). `go gate.Run(runtimeInstance)` discards the returned refusal, which is still reported through `OnCampaignError` or the logger; read the return value to act on it.

### Session: the in-memory and file storages hold at most 100,000 sessions

**What changed.** [`InMemoryStorageConfig.MaxSessions`](../session/in_memory_storage.go) and [`FileStorageConfig.MaxSessions`](../session/file_storage.go) are `bound.Count` fields of 100,000 when left out, at least 1, unlimited admitted. At the ceiling, once the lapsed sessions are swept, a `Save` of a new id is refused and a `Save` of a stored id still lands. A value below the minimum panics in `NewInMemoryStorage` and is returned as an error by `NewFileStorage`. The third major has no ceiling, and its table grows with every session written.

**Symptom.** A process at 100,000 live sessions answers an empty 503 with `Retry-After` and no session cookie to a request that writes to a new session, unless its response is already committed, where the refusal is only logged, with `the in-memory session storage is full` or `the session file storage is full` in its journal.

**Remedy.** Register the storage with a larger `MaxSessions`, or `bound.UnlimitedCount()` for the third major's behaviour, or move to a shared storage. The framework's fallback storage reads its count from `MELODY_HTTP_SESSION_MAX_SESSIONS`.

### Container: a scope's Close runs under a term, and the close doors are on the contracts

**What changed.** A scope's close hands its scoped services a context that ends at [`ContainerConfig.ScopeTeardownTimeout`](../container/container.go), a `bound.Duration` of 3s when left out, at least 1ms, unlimited admitted for no term; the third major closed it with no term. The application declares it through `ApplicationConfig.ScopeTeardownTimeout` and the key `MELODY_SCOPE_TEARDOWN_TIMEOUT`, the key winning, and this is the close of a request's, a command's, a consumed message's and a cron run's scope. `CloseWithContext` is declared on the `Container` and `Scope` contracts, and on a scope it takes the caller's term in place of the configured one. [`Application.CloseWithContext`](../application/application_close.go) is new, and returns the teardown failure this call discovered. A nil context on any of the three doors is refused with an error, and leaves the teardown to a later call. `KernelConfiguration` declares `ScopeTeardownTimeout`.

An http process refuses to boot unless the term stays strictly under `MELODY_HTTP_SHUTDOWN_TIMEOUT`, the default of 3s included.

**Symptom.** A scoped service whose close takes longer than 3s receives a context that has ended, and its failure is reported by the scope's close, where the close used to wait for it. An http process with `MELODY_HTTP_SHUTDOWN_TIMEOUT` at 3s or less, or an unlimited scope term, ends its boot with `the scope teardown timeout must stay under the http shutdown timeout`. A value of your own implementing `Container` or `Scope` no longer compiles without `CloseWithContext`, nor does a `KernelConfiguration` of your own without `ScopeTeardownTimeout`.

**Remedy.** A scoped service that legitimately needs longer takes a larger `MELODY_SCOPE_TEARDOWN_TIMEOUT`, kept under the http shutdown timeout, which grows with it; `unlimited`, the third major's behaviour, is for a process that serves no http. A shutdown timeout of 3s or less takes a shorter scope teardown timeout beside it. Declare `CloseWithContext` on your doubles, and answer `ScopeTeardownTimeout() bound.Duration` as the key read, absent when unset.

### Container: a container is built from one configuration struct, and a resolution waiting on another goroutine's creation is bounded and released by Close

**What changed.** [`NewContainer`](../container/container.go) takes a [`ContainerConfig`](../container/container.go), whose zero value is the default. Its field `CreationWaiters` is a `bound.Count` that bounds the resolutions that may wait on one service another goroutine is building: 1,024 when left out, at least 1, and unlimited admitted. One more waiter is refused at once with [`ErrCreationWaitersExceeded`](../container/errors.go). A value below 1 panics at `NewContainer`, naming `ContainerConfig.CreationWaiters`. The application declares the bound through `ApplicationConfig.ContainerCreationWaiters` and the key `MELODY_CONTAINER_CREATION_WAITERS` (`kernel.container_creation_waiters`), the key winning over the field. [`KernelConfiguration`](../config/contract/kernel.go) declares `ContainerCreationWaiters`, the key as the deployment set it.

A waiter is released by what it waits in. The container's `Close` releases every waiter with `ErrContainerClosed` the moment the teardown begins. A scope's `Close` releases the waiters on that scope's own creations with `ErrScopeClosed`. A creation that finished in the same instant is still answered as its value. In the third major a waiter parked until the provider returned, so a provider that never returned held its waiters past `Close`, and a resolution of the service arriving while the teardown ran parked beside them; only once the teardown finished was a later resolution refused.

**Symptom.** `container.NewContainer()` no longer compiles, nor does a `KernelConfiguration` of your own that does not declare `ContainerCreationWaiters`. A resolution that reaches a creation already holding 1,024 waiters is refused where it waited. A resolution parked during a shutdown or a scope's close returns the closed error where it kept waiting.

**Remedy.** Write `container.NewContainer(container.ContainerConfig{})`. Declare `ContainerCreationWaiters() bound.Count` on your configuration and answer the key as read, absent when unset. A deployment whose cold start fans more concurrent resolutions into one service raises the bound, or sets `unlimited`.

### Container: the resolver doors take the caller's context first, and a resolution waiting on another goroutine's creation leaves when it ends

**What changed.** [`Resolver`](../container/contract/resolver.go)'s `Get`, `MustGet`, `GetByType` and `MustGetByType` take a `context.Context` first, on the container, a scope and the resolver a provider receives; `Has` and `HasType` do not change. So do [`FromResolver`, `MustFromResolver`, `FromResolverByType`, `MustFromResolverByType`](../container/resolver.go), [`AllImplementing` and `MustAllImplementing`](../container/resolver_implementing.go), a [`LazyService`](../container/lazy.go)'s `Resolve` and `Get` (the `Lazy` and `LazyByType` builders do not), and every framework `*FromResolver` helper, the integrations' included. A resolution parked on a creation another goroutine runs leaves the moment its context ends, answering an error whose cause is `context.Cause(ctx)`; a creation that finished in the same instant is answered as its value, and the provider is never interrupted; a provider's own resolutions wait under its first caller's context, so a provider that waits on a nested creation fails with that caller, and the resolutions coalesced behind it answer the failure. A nil context, a typed nil included, is refused at every one of these doors with `context is required in …` naming the door. `Provider[T]` keeps its signature: the resolver a provider receives carries, while the provider runs, the context of the call whose resolution started the creation, read through [`resolver.Runtime().Context()`](../container/contract/resolver.go); the container, a scope and a resolver retained after its provider returned answer a detached runtime over `context.Background()`. `runtime.FromRuntime`, `runtime.MustFromRuntime` and the `*FromRuntime` helpers keep their shape and resolve under `runtimeInstance.Context()`. [`kernelcontract.Kernel`](../kernel/contract/kernel.go) declares `BootRuntime()`, the runtime a boot hook resolves under. The encrypt module's `DatabaseFactory` and `EncryptDatabaseCommandConfig.DatabaseResolver` take the command run's runtime, `func(runtimeInstance containercontract.Runtime) (*bun.DB, error)`, and resolve through `runtime.FromRuntime[*bun.DB](runtimeInstance, name)`; the encrypt `Migrator` doors take the runtime first. The audit recorder, tracker, storages and `BeginTransaction` and `migrate.Up`, `Down` and `RunQueries` stay on a context: they run inside a bun transaction or a bun migration function, whose handle is the context. The wiring generator writes `FromResolverByType[T](resolver.Runtime().Context(), resolver)` and `ConfigMustFromResolver(resolver.Runtime().Context(), resolver)`. In the third major no resolution took a context, and a waiter parked until the provider returned whatever its caller had given up on.

**Symptom.** `serviceContainer.Get("app.service")`, `container.MustFromResolver[T](resolver, name)`, `container.FromResolverByType[T](resolver)`, `container.AllImplementing[T](resolver)`, `handle.Resolve()`, `logging.LoggerMustFromResolver(serviceContainer)`, `config.ConfigMustFromResolver(resolver)` and `kernel.NewKernel(configuration, ...)` no longer compile, nor does a resolver or a kernel of your own that does not declare the new forms. A previously generated `wiring_gen.go` no longer compiles until it is regenerated.

**Remedy.** Hand the context you hold: `serviceContainer.Get(ctx, "app.service")`. Inside a provider write `ctx := resolver.Runtime().Context()` and pass it on, `container.MustFromResolver[T](ctx, resolver, name)`; in a command or a handler resolve through the runtime, `runtime.MustFromRuntime[T](runtimeInstance, name)`, `runtime.FromRuntimeByType[T](runtimeInstance)` by type, `runtime.FromRuntimeByReflectType(runtimeInstance, targetType)` for a type held as a value and `runtime.AllImplementingFromRuntime[T](runtimeInstance)` for a collection, never the resolver form handed `runtimeInstance.Context()` and `runtimeInstance.Scope()` apart; at boot pass the application's context, or resolve under `kernelInstance.BootRuntime()` in a module hook. A test passes `t.Context()`. Regenerate the wiring with `go generate ./generated`. A resolver of your own adds the context to its four waiting doors and declares `Runtime() containercontract.Runtime`; a kernel of your own declares `BootRuntime()`.

### Container: no ring of dependencies is admitted, a ring closed through a Lazy handle included

**What changed.** The container refuses every edge that would close a ring of dependencies, at the moment it would be written, and every refusal of a ring carries the new [`ErrCircularDependency`](../container/errors.go): the cycle inside one resolution, two concurrent resolutions waiting on each other, the resolution that closes a ring through a [`Lazy`](../container/lazy.go) handle or a retained resolver, and a `TeardownDependencyNames` or `TeardownDependencyTypes` declaration, at `Register` or `DeclareTeardownDependency`, closing a ring with the edges already recorded; a resolution closing a ring with a declaration is refused too, and so is the registration of the one service a type declaration names when the declaration then closes a ring. A name and the one name its type stands for are one node, and a lookup that fails leaves no edge behind. In the third major the edge a Lazy handle wrote after its owner was built was weak: a ring closed through it was admitted and ordered at the teardown by the edges the providers wrote, a declared ring was admitted and reported as a dependency cycle at `Close`, and the three ring refusals carried no cause.

**Symptom.** A Lazy handle, or a resolver kept past its provider, whose first use builds a service that resolves the holder answers an error whose cause is `ErrCircularDependency`; that service is not built unless it was built first. A registration or a `DeclareTeardownDependency` whose declaration closes a ring with what is already recorded is refused. A teardown that reported `dependency cycle detected` over declared edges no longer runs: the declaration that closed the ring is refused first.

**Remedy.** Break the ring in the wiring: the service built over the holder takes what it needs from somewhere other than the holder, or the holder stops resolving it. Drop the declaration that names the service the other one already depends on. Match the refusal with `errors.Is(err, container.ErrCircularDependency)` rather than its message.

### Container: `CloseWithContext` is the one teardown door, and a service carrying `Close` alone is refused

**What changed.** The teardown closes a service through `CloseWithContext(closeContext context.Context) error` alone, handing it what is left of the budget; the third major closed a value carrying only `Close() error` through it, bounded by nothing. Such a value is refused with the new [`ErrCloseDoorRequired`](../container/errors.go) before it can be filed: at the registration whose provider declares a concrete type carrying it — `Register`, `RegisterType` and both scoped registrations, on the container and on a scope — at an override the container or the scope would close, and at the creation behind a provider declaring an interface, where the refused value's `Close` is called once and its failure joined to the refusal. A [`RegisterOptions.Closer`](../container/register_option.go) admits it. The framework's service contracts follow: `cache/contract.Cache` and `Backend`, `session/contract.Storage` and `Manager`, `messagebus/contract.Transport` and `http.ServerSentEventBackplane` declare `CloseWithContext` in place of `Close`, and every framework service the container closes — the cache and session managers and storages, the in-memory transport and `TransportsCloser`, `httpclient.HttpClient`, `http.ServerSentEventHub`, the json logger, and the integrations' connections, backends, backplanes, managers, registries and recorders — carries `CloseWithContext` and no `Close`.

**Symptom.** A registration or an override of a type with `Close() error` and no `CloseWithContext` answers an error whose cause is `ErrCloseDoorRequired`, "the service's type carries Close without CloseWithContext", and the `Must` forms panic with it; a provider declaring an interface whose value carries `Close` alone fails its resolution with the same cause. A type written against one of the six contracts no longer satisfies it, and a call to `Close()` on one of the framework's services no longer compiles.

**Remedy.** Rename the service's `Close() error` into `CloseWithContext(closeContext context.Context) error`, and where it waits — a goroutine to join, a flush, a socket — wait under `closeContext` and answer its error past the deadline. A type you do not own, `*sql.DB`, `*bun.DB`, `*os.File` or a driver's client, is registered with `RegisterOptions{Closer: container.NewCloser(func(closeContext context.Context, value *T) error { ... })}`; a scoped value of such a type is wrapped in a type of your own, since a scoped registration refuses a closer. Where you called `Close()` on a framework service, call `CloseWithContext(ctx)` with the context in scope, `context.Background()` at process exit.

### Container: the teardown closes in dependency waves by default, and `ArmParallelTeardown` is gone

**What changed.** The container closes, in one wave, every service its teardown graph proves unrelated to the others in that wave, from the moment it is built; the third major closed service by service unless the composition root called `ArmParallelTeardown()`, which is removed. [`ContainerConfig.TeardownSequential`](../container/container.go) restores the sequential order, and an application declares it through `ApplicationConfig.ContainerTeardownSequential` or the new key `MELODY_CONTAINER_TEARDOWN_SEQUENTIAL` (`kernel.container_teardown_sequential`), the key winning; [`KernelConfiguration`](../config/contract/kernel.go) declares `ContainerTeardownSequential()`. The walk of what each service holds runs where each service is filed, as it did once armed. The application's url generator, logger, session storage and session manager resolve the clock, the configuration and the route registry through the container, where they captured the kernel's values, so their teardown edges are recorded and an override of those services reaches them.

**Symptom.** Two services with no edge between them start their `CloseWithContext` at the same moment, where the third major closed the newer one first; a service whose close relied on that order — a worker reporting its drain through a collaborator it captured instead of resolving — may find the collaborator already closed. A type assertion on `ArmParallelTeardown` no longer matches, and a type implementing `config/contract.KernelConfiguration` no longer satisfies it.

**Remedy.** Write the order down: resolve the collaborator inside the provider, or declare it in `RegisterOptions.TeardownDependencyNames`. Where two `CloseWithContext` methods touch the same external state the container cannot see, declare the edge between them or set `ContainerTeardownSequential`. Remove the arming block from the composition root, and add `ContainerTeardownSequential() configcontract.Switch` to a `KernelConfiguration` of your own. `debug:container` prints the plan: the rows whose `ordering` reads `none` are the services nothing orders.

### Container: a declared teardown dependency naming no service fails the boot

**What changed.** A `TeardownDependencyNames` or `TeardownDependencyTypes` entry naming a service nothing registered is refused with `ErrTeardownDependencyWasNeverRegistered` by the new `VerifyDeclaredTeardownDependencies() error` on the concrete container, which the application calls at the end of its boot, before `Run`; a container used without an application reports it at `Close`, which then closes one service after the other and joins the refusal to its answer. A declaration naming a scoped service is refused with `ErrTeardownDependencyIsScoped`, at the registration when the scoped service is already registered; one keyed by a type more than one service is registered under with `ErrTeardownDependencyTypeIsAmbiguous`, at the registration. A name may still be declared before its service is registered. In the third major an unarmed container dropped such a declaration in silence, and an armed one refused a declaration whose dependency was not registered yet. The new `RegisterOptions.OptionalTeardownDependencyNames` and `OptionalTeardownDependencyTypes` declare a collaborator that may be absent: the edge orders the teardown when it is registered and is dropped without a refusal when it is not. Under `ContainerTeardownSequential` none of these is refused.

**Symptom.** The boot exits with "a declared teardown dependency does not name one container service", carrying the declaring service and the name it declared. A bare container's `Close` answers an error whose cause is `ErrTeardownDependencyWasNeverRegistered`.

**Remedy.** Fix the misspelled name. A collaborator registered only in some deployments moves to `OptionalTeardownDependencyNames`, or is declared only where it is registered.

### Lock: the lazy locker resolves the registered locker at the lock's first method, under that method's runtime

**What changed.** A lock from [`lock.NewLazyLocker`](../lock/lazy_locker.go) resolves `service.lock.locker` at its first `Acquire`, `Release` or `Refresh`, under the runtime handed to it — its context and its kind — and creates the real lock then; `CreateLock` resolves nothing. A failed resolution is answered by the method that met it and retried at the next call, where the third major resolved at `CreateLock` and retried at the next `CreateLock`. The `Locker` and `Lock` contracts do not change.

**Symptom.** None at compile time. A locker provider runs at the first `Acquire` rather than at `CreateLock`, and a run whose context ends while another goroutine builds the locker leaves the wait with its context's error.

**Remedy.** None needed. Code that relied on `CreateLock` building the locker resolves `service.lock.locker` itself.

### Application: a middleware module installs through one `Use`, and the application's middleware doors take slices

**What changed.** [`HttpMiddlewareRegistrar`](../application/contract/http_middleware_module.go) declares one `Use`, taking a [`MiddlewareRegistration`](../application/contract/http_middleware_module.go): `Priority`, `Middlewares` and `Factories`, the zero priority `MiddlewarePriorityDefault`, the lists installed in the order given, middlewares first, a nil entry in either panicking naming the field. A module can now register a factory through the registrar, which the third major kept off the contract. `(*HttpMiddleware).UseWithPriority`, `UseFactories` and `UseFactoriesWithPriority` are removed. `Application.RegisterHttpMiddlewares` and `RegisterHttpMiddlewareFactories` take slices, and `MiddlewareFactory` is declared in `application/contract`, `application.MiddlewareFactory` naming the same type.

**Symptom.** `registrar.Use(middleware)`, `registrar.UseWithPriority(10, middleware)`, `app.RegisterHttpMiddlewares(a, b)` and a registrar double declaring the two variadic methods no longer compile.

**Remedy.** `registrar.Use(applicationcontract.MiddlewareRegistration{Middlewares: []httpcontract.Middleware{middleware}})`, `{Priority: 10, Middlewares: ...}` where a priority was stated, `{Factories: []applicationcontract.MiddlewareFactory{factory}}` for a factory, and `app.RegisterHttpMiddlewares([]httpcontract.Middleware{a, b})`.

### Http: a route declares its options in one struct, and `NewRequirements` takes a slice

**What changed.** [`RouteOptions`](../http/contract/route_option.go) is a struct of nine fields, `Name`, `Methods`, `Host`, `Schemes`, `Requirements`, `Defaults`, `Locales`, `Priority` and `Attributes`, whose zero value registers a route under no name for every method, host and scheme; `Handle` takes it by value and copies its lists and maps at registration, so the value can be reused; `http.RouteOptions` names the same type. The third major declared a twelve-method interface built by `NewRouteOptions` over nine positional arguments. [`NewRequirements`](../http/constraint.go) takes a `[]*Requirement`, nil declaring none.

**Symptom.** `http.NewRouteOptions(name, methods, ...)`, the accessors and setters (`options.Name()`, `options.SetRequirements(...)`), `Handle(pattern, handler, nil)` and `NewRequirements(a, b)` no longer compile.

**Remedy.** `http.RouteOptions{Name: "api.users", Methods: []string{"GET"}}`, `Handle(pattern, handler, http.RouteOptions{})` for a route with no option, a field write where a setter was called, and `NewRequirements([]*http.Requirement{a, b})`.

### Http: the server-sent event writer re-arms a 30s write deadline by default, and the hub is built from one struct

**What changed.** [`NewServerSentEventWriter`](../http/server_sent_event.go) takes a `ServerSentEventWriterConfig`, whose `WriteBudget` is a `bound.Duration`: 30s when left out, at least 1ms, and `bound.UnlimitedDuration()` the third major's reading, the deadline the server armed alone. A refused budget is answered as the door's error before the headers are committed. `WithWriteBudget` is removed. [`NewServerSentEventHub`](../http/server_sent_event_hub.go) takes a `ServerSentEventHubConfig`: `SubscriberBuffer`, 16 when left out and at least 1, unlimited refused, a refusal panicking naming the field, and `Logger`. `Subscribe` takes the topic alone, every subscriber buffered as the hub declares.

**Symptom.** `NewServerSentEventWriter(writer)`, `writer.WithWriteBudget(d)`, `NewServerSentEventHub()` and `hub.Subscribe(topic, 16)` no longer compile. A stream whose handler set no budget is now cut 30s after the first frame a client that stopped reading cannot take, where it ran until the server's own `WriteTimeout`, or forever without one.

**Remedy.** `NewServerSentEventWriter(writer, http.ServerSentEventWriterConfig{WriteBudget: bound.Of(d)})`, `{WriteBudget: bound.UnlimitedDuration()}` for the third major's reading; `NewServerSentEventHub(http.ServerSentEventHubConfig{})`, `{SubscriberBuffer: bound.Of(64)}` where the subscribers passed 64; `hub.Subscribe(topic)`.

### Http: `DeleteCookie` takes the cookie

**What changed.** One [`DeleteCookie`](../http/cookie.go)`(response, *nethttp.Cookie)` expires the cookie its `Name`, `Path` and `Domain` name, the same value `SetCookie` takes, and refuses a nil cookie; every other field is ignored. `DeleteCookieWithDomain` is removed.

**Symptom.** `DeleteCookie(response, "session", "/")` and `DeleteCookieWithDomain(response, "session", "/", "example.com")` no longer compile.

**Remedy.** `DeleteCookie(response, &nethttp.Cookie{Name: "session", Path: "/"})`, with `Domain: "example.com"` for a cookie set on a domain.

### Http: `JsonHandler` takes a configuration struct

**What changed.** [`JsonHandler`](../http/typed_handler.go)`[RequestBody](handle, JsonHandlerConfig)` takes the struct last, `ErrorResponder` its one field, nil leaving the refusals to the framework. `JsonHandlerOption` and `WithJsonHandlerErrorResponder` are removed.

**Symptom.** `JsonHandler(handle)` and `JsonHandler(handle, WithJsonHandlerErrorResponder(responder))` no longer compile.

**Remedy.** `JsonHandler(handle, http.JsonHandlerConfig{})`, and `http.JsonHandlerConfig{ErrorResponder: responder}`.

### Websocket: the subscribe buffer is the hub's

**What changed.** The websocket integration's `Options.SubscribeBuffer` (the struct is `StreamHandlerConfig` in this major) is removed: the stream handler subscribes with the hub's `SubscriberBuffer`, since `ServerSentEventHub.Subscribe` takes the topic alone.

**Symptom.** `websocket.Options{SubscribeBuffer: 64}` no longer compiles.

**Remedy.** Declare the size on the hub: `http.NewServerSentEventHub(http.ServerSentEventHubConfig{SubscriberBuffer: bound.Of(64)})`.

### Http: the cors preflight max age is a bound count, the default doors are gone, and a nil service is refused

**What changed.** [`cors.Config`](../http/cors/service.go)`.MaxAge` is a `bound.Count`: 86400 when left out, at least 0, unlimited refused, written as `Access-Control-Max-Age` on every preflight answer, 0 included, which tells the browser to cache nothing. `NewService(Config{})` is the third major's `DefaultService()`. `DefaultService`, `DefaultMiddleware` and `Restrictive(...string)` are removed; `RestrictiveService([]string)` stays as the credentialed preset. [`Middleware`](../http/cors/middleware.go), `RegisterListeners`, `RegisterRequestListener` and `RegisterResponseListener` refuse a nil service naming the door, where the third major read it as the default service.

**Symptom.** `cors.Config{MaxAge: 600}`, `cors.DefaultService()`, `cors.DefaultMiddleware()` and `cors.Restrictive("https://a.example")` no longer compile; `cors.Middleware(nil)` panics. A `Config{}` that wrote no `Access-Control-Max-Age`, leaving the browser's own short default, now advertises a day, and a max age of 0 is written as `0` where it was left off the answer.

**Remedy.** `MaxAge: bound.Of(600)`; `cors.NewService(cors.Config{})` for the default service and `cors.Middleware(cors.NewService(cors.Config{}))` for the default middleware; `cors.Middleware(cors.RestrictiveService([]string{"https://a.example"}))`; `MaxAge: bound.Of(0)` for a browser that asks again on every request.

### Http: a credentialed cors service refuses an origin without a scheme

**What changed.** [`cors.NewService`](../http/cors/service.go) panics when a service with `AllowCredentials` and no `AllowOrigin` holds an allowed origin without a scheme, `example.com` or `*.example.com`, naming `Config.AllowOrigins` and the entry: such an entry admits its host under any scheme, `http://` included, so a page served in clear text received the credentialed answer. `RestrictiveService` is credentialed and refuses it too. Without credentials, or behind an `AllowOrigin`, a scheme-less entry keeps its any-scheme reading. The third major named such an entry in the `cors.credentialedOriginWithoutScheme` boot warning at the service's first request, which is removed.

**Symptom.** The boot panics with "cors misconfiguration: a credentialed service admits an origin without a scheme under any scheme".

**Remedy.** Write the scheme out, `https://example.com` or `https://*.example.com`; an origin meant to be served over plain http is written `http://` explicitly.

### Http: the deprecated middleware cors family is removed

**What changed.** `middleware.CorsConfig`, `NewCorsConfig`, `DefaultCorsConfig`, `RestrictiveCorsConfig`, `CorsMiddleware`, `DefaultCorsMiddleware` and `RestrictiveCors`, deprecated since the third major, are removed.

**Symptom.** They no longer compile.

**Remedy.** The [`http/cors`](../http/cors) package, as the deprecation said: `cors.Middleware(cors.NewService(cors.Config{...}))`.

### Http: the compression middleware is built from one struct, and a nil exclusion list is the default family

**What changed.** [`CompressionMiddleware`](../http/middleware/compression.go) takes a `CompressionConfig` value whose zero value is the third major's `DefaultCompressionConfig()`: `Level`, gzip's level, 0 and -1 the default, -2 to 9 kept, any other value refused; `MinSize`, a `bound.Count`, 1024 when left out, at least 1, unlimited refused; `ExcludedContentTypes`, nil the media and archive family (`image/`, `video/`, `audio/`, `application/zip`, `application/gzip`, `application/x-gzip`) and an empty list none; `ExcludedPaths`. The middleware copies the struct when it is built, and a refused field panics naming it. `NewCompressionConfig`, its accessors and setters, `DefaultCompressionConfig` and `DefaultCompressionMiddleware` are removed.

**Symptom.** `NewCompressionConfig(6, 1024, nil, nil)`, `DefaultCompressionMiddleware()` and `CompressionMiddleware(nil)` no longer compile. A configuration that passed nil content types and relied on `image/*` being compressed no longer compresses it. A `MinSize` of 0 or below, or a `Level` outside -2..9, panics where it read as the default.

**Remedy.** `CompressionMiddleware(middleware.CompressionConfig{})` for the default; `{Level: 6, MinSize: bound.Of(2048)}` for a level and a threshold; `ExcludedContentTypes: []string{}` to compress every type.

### Http: the in-process rate limiters take a name, a limit and a window, and a `LimiterConfig`

**What changed.** [`NewFixedWindowLimiter(name, limit, window, config)`](../http/middleware/rate_limit.go) and `NewSlidingWindowLimiter(name, limit, window, config)` take the name, the limit and the window positionally, the mandatory values, and one `LimiterConfig` of what has a default: `MaxKeys`, a `bound.Count`, 1,000,000 when left out, at least 1, unlimited refused; `CleanupInterval`, a `bound.Duration`, the cadence the idle keys are forgotten at, 5m when left out, at least 1ms, unlimited refused; `Clock`, nil the system clock. The name is 1 to 64 bytes of `[A-Za-z0-9._-]`, the identity the Redis limiter keys its counters by and the in-memory ones answer through `Name()`; a malformed name, a limit below 1 and a window below 1ms panic naming the argument. The limiter reads the struct once, when it is built. The two `WithClock` constructors, the deprecated `TokenBucketLimiter` with its two constructors, `SetMaxKeys` and `SetEvictOldestKeyAtCeiling` are removed; a full table always evicts its least recently seen key (see "Http: a full limiter table evicts its least recently seen key").

**Symptom.** `NewFixedWindowLimiter(100, time.Minute)`, `NewSlidingWindowLimiterWithClock(clock, 10, window)`, `limiter.SetMaxKeys(10_000)` and `limiter.SetEvictOldestKeyAtCeiling(true)` no longer compile: `not enough arguments in call to NewFixedWindowLimiter`. A limiter built with a limit or a window of 0 panics, where it limited to one request per minute; a `MaxKeys` of 0 panics, where `SetMaxKeys(0)` was ignored.

**Remedy.** `NewSlidingWindowLimiter("login", 10, window, middleware.LimiterConfig{MaxKeys: bound.Of(10_000), Clock: clock})`; drop `SetEvictOldestKeyAtCeiling(true)`, since the limiter always evicts.

### Http: the rate limit doors take the limiter positionally and a `RateLimitConfig` value read once, and the quick limit doors take a struct

**What changed.** `RateLimitMiddleware(limiter, config)` and `RegisterRateLimitRequestListener(eventDispatcher, limiter, config)` take the limiter positionally, a nil or typed-nil one refused naming the argument and the door, and a [`RateLimitConfig`](../http/middleware/rate_limit.go) struct of exported fields, `KeyExtractor`, `OnLimitExceeded` and `ClientIpResolver`, by value, read once, when they are built, where the third major read the pointer on every request, so a setter changed the middleware in flight; the zero value is the default. `SimpleRateLimit` and `IpRateLimit` take a `QuickLimitConfig`, `RequestsPerMinute` and `ClientIpResolver`, and `UserRateLimit` a `UserLimitConfig`, `RequestsPerMinute`, `UserId` and `ClientIpResolver`; `RequestsPerMinute` below 1 is refused naming the field, where it read as one. `NewRateLimitConfig`, its accessors and setters and the three `WithResolver` doors are removed.

**Symptom.** `NewRateLimitConfig(limiter, nil, nil)`, `config.SetClientIpResolver(resolver)`, `RateLimitMiddleware(nil)`, `RegisterRateLimitRequestListener(eventDispatcher, config)`, `SimpleRateLimit(60)` and `UserRateLimitWithResolver(60, userId, resolver)` no longer compile. A field written into the configuration after the door was built no longer reaches it.

**Remedy.** `RateLimitMiddleware(limiter, middleware.RateLimitConfig{ClientIpResolver: resolver})`, `RegisterRateLimitRequestListener(eventDispatcher, limiter, middleware.RateLimitConfig{})`; `SimpleRateLimit(middleware.QuickLimitConfig{RequestsPerMinute: 60, ClientIpResolver: resolver})`; `UserRateLimit(middleware.UserLimitConfig{RequestsPerMinute: 60, UserId: userId, ClientIpResolver: resolver})`. A middleware that needs another configuration is built again from it.

### Http: the middleware pipeline takes value definitions in slices

**What changed.** [`pipeline.HttpMiddlewareDefinition`](../http/middleware/pipeline/definition.go) is a struct of exported fields, `Name`, `Priority`, `Before`, `After`, `Groups`, `EnabledEnvironments`, `Factory`, `ReplaceExisting`, `AllowDuplicates` and `FunctionName`; [`NewBuilder`](../http/middleware/pipeline/builder.go) and `Add` take a `[]HttpMiddlewareDefinition`, nil taking none, and copy each definition's lists as they take it. `NewHttpMiddlewareDefinition` and `SetFunctionName` are removed.

**Symptom.** `NewHttpMiddlewareDefinition(name, 10, nil, nil, groups, nil, factory, false, true)`, `definition.SetFunctionName(name)` and `NewBuilder(a, b)` no longer compile.

**Remedy.** `pipeline.HttpMiddlewareDefinition{Name: name, Priority: 10, Groups: groups, Factory: factory, AllowDuplicates: true, FunctionName: name}`; `NewBuilder([]pipeline.HttpMiddlewareDefinition{a, b})`.

### Cache: the in-memory backend is built from one struct and bounded at 10,000 entries by default

**What changed.** [`NewInMemoryBackend`](../cache/in_memory.go) takes an `InMemoryBackendConfig`: `MaxItems` is a `bound.Count` of 10,000 when left out, at least 1, `bound.UnlimitedCount()` lifting the ceiling; `CleanupInterval` is a `bound.Duration` of 1m when left out, at least 1ms, unlimited refused; `Clock` nil is the system clock. A field below its minimum panics at construction, naming it. The third major read a `maxItems` of 0 as no ceiling and a non-positive interval as a minute, and refused a nil clock. The application's default backend takes the default ceiling, so the boot warning about an unbounded default cache and `(*messagebus.ConsumeCommand).IsLongRunning`, which only fed it, are gone.

**Symptom.** `NewInMemoryBackend(0, 0, clock)` no longer compiles. A backend, the application's default one included, that held every entry evicts the least recently promoted one past 10,000.

**Remedy.** `cache.NewInMemoryBackend(cache.InMemoryBackendConfig{Clock: clock})`; `MaxItems: bound.UnlimitedCount()` keeps the third major's unbounded reading, and `MaxItems: bound.Of(n)` sets the ceiling.

### Cache: the manager is built from one door, and `Remember` takes the bound cache first and one struct

**What changed.** [`NewManager`](../cache/manager.go) takes a `ManagerConfig` after the backend and the serializer, `OwnsBackend` its one field, and `NewManagerOwningBackend` is removed. [`Remember`](../cache/remember.go) takes the bound cache first and a `RememberConfig` before the callback: `Ttl` is required (see the next section), `StampedeProtectionDisabled` turns the flight off, `WaitTimeout` bounds this caller's wait at 30s when left out, `bound.Of(time.Duration(0))` taking a memoized answer or none at once and `bound.UnlimitedDuration()` waiting on the flight and the context alone, and `Detached` keeps the flight running when its last waiter leaves. An absent runtime is refused, and an absent or too short ttl and a negative wait are answered as the error naming the field, all before the cache is read. `RememberOption`, `NewDefaultRememberOption` and their setters and getters are removed.

**Symptom.** `NewManagerOwningBackend(b, s)`, `NewManager(b, s)`, `Remember(cache, key, 0, callback, nil)` and `NewDefaultRememberOption().WithWaitTimeout(d)` no longer compile. A caller that waited on a flight without a bound is answered a wait-timeout error after 30s.

**Remedy.** `cache.NewManager(b, s, cache.ManagerConfig{OwnsBackend: true})`; `cache.Remember(cache.FromRuntime(runtimeInstance), key, cache.RememberConfig{Ttl: bound.UnlimitedDuration()}, callback)`, the request's runtime where one is served; `WaitTimeout: bound.UnlimitedDuration()` for the unbounded wait and `Detached: true` for `WithCancelable(false)`.

### Cache: a write's ttl is a bound limit, and no expiry is written explicitly

**What changed.** `Backend.Set`, `Backend.SetMultiple`, `Cache.Set` and `Cache.SetMultiple` take the ttl as a `bound.Duration`, read through [`cachecontract.ResolveTtl`](../cache/contract/ttl.go) against `cachecontract.TtlRule()`, so every backend refuses the same ttls with the same answer: an absent ttl is refused, a value is at least 1s, and `bound.UnlimitedDuration()` stores an entry with no expiry. The third major read a ttl of 0 as no expiry and refused only a negative one; the in-memory and the redis backends both follow.

**Symptom.** `cache.Set(key, value, 0)`, `cache.Set(key, value, time.Minute)` and a backend or cache declaring `Set(key string, payload []byte, ttl time.Duration)` no longer compile. A ttl under a second is refused, naming the ttl.

**Remedy.** `bound.Of(ttl)` for a lifetime, `bound.UnlimitedDuration()` for no expiry. A backend of the application's own takes the `bound.Duration` and reads it through `cachecontract.ResolveTtl`, which answers 0 for no expiry.

### Session: the manager and the two storages are built from one struct each

**What changed.** [`NewManager`](../session/manager.go) takes a `ManagerConfig` after the storage: `Ttl` is a `bound.Duration` of 24h when left out, at least 1s, `bound.UnlimitedDuration()` for sessions that never lapse; `TombstoneRetention` is a `bound.Duration` of 5m when left out, at least 1s, unlimited refused; `Clock` nil is the system clock, on the manager and on both storages; `OwnsStorage` closes the storage with the manager. A field below its minimum panics at construction, naming it. The third major read a ttl of 0 as no expiry, refused a retention only when it was not positive and refused a nil clock on the manager and on both storages. [`NewInMemoryStorage`](../session/in_memory_storage.go) takes an `InMemoryStorageConfig`, `CleanupInterval` 1m when left out, at least 1ms, unlimited refused, `MaxSessions`, and `Clock`. [`NewFileStorage`](../session/file_storage.go) takes a `FileStorageConfig` naming exactly one of `Path`, a file the storage owns, and `File`, a handle the caller owns, `MaxSessions`, and `Clock`; both, or neither, is answered as the error, an empty `Path` reading as not set, where the third major answered it as an invalid path. `NewManagerWithTombstoneRetention`, `NewManagerOwningStorage`, `NewManagerWithClock`, `NewInMemoryStorageWithCleanupInterval`, `NewInMemoryStorageWithClock`, `NewFileStorageFromPath`, `NewFileStorageFromPathWithClock`, `NewFileStorageFromFile`, `NewFileStorageFromFileWithClock` and the `TombstoneRetention` constant are removed. The application wires the configured session ttl and tombstone retention through, so a deployment configuring them sees no change.

**Symptom.** `NewManager(storage, ttl)`, `NewManagerWithClock(...)`, `NewInMemoryStorage()` and `NewFileStorageFromPath(p)` no longer compile. A manager built by hand with no ttl expires its sessions after 24h, and a tombstone retention under a second panics where the third major built the manager.

**Remedy.** `session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(ttl)})`, `Ttl: bound.UnlimitedDuration()` for the third major's 0; `OwnsStorage: true` for `NewManagerOwningStorage`; `session.NewInMemoryStorage(session.InMemoryStorageConfig{})`; `session.NewFileStorage(session.FileStorageConfig{Path: p})` or `{File: f}`; `config.DefaultSessionTombstoneRetention` names the five minutes the removed constant held.

### Lock: the ttl is a bound limit, and the exclusive run and the leader gate are built from one struct each

**What changed.** `Locker.CreateLock` and `Lock.Refresh` take the ttl as a `bound.Duration`, read against [`lockcontract.LockTtlRule()`](../lock/contract/lock.go): 30s when left out, at least 1ms, `bound.UnlimitedDuration()` the session-style lock that holds until its connection drops. The lease lockers, the in-memory one and the redis one, resolve it through `lockcontract.ResolveLeaseTtl` at `Acquire` and `lockcontract.ResolveLeaseRefreshTtl` at `Refresh` and answer a ttl under 1ms or an unlimited one as the error, naming `Locker.CreateLock.ttl` or `Lock.Refresh.ttl`; the mysql and pgsql lockers ignore it, every state admitted. The third major's in-memory locker read a ttl of 0 at `CreateLock` as a lock that never expires, so a holder that crashed held it for the process's life; every in-memory lock now lapses. [`RunExclusive`](../lock/run_exclusive.go) takes an `ExclusiveConfig` after the lock name, `Ttl` 30s when left out, at least 2ms, unlimited selecting the session-style probe, and `ReleaseTimeout` 5s, at least 1ms, unlimited refused, both resolved before the lock is created and refused as the error it returns. [`NewExclusiveCommand`](../lock/exclusive_command.go) takes an `ExclusiveCommandConfig`, `LockName`, empty for `"melody:command:"` plus the command's name, `Ttl` and `ReleaseTimeout`, resolved when the command is built. [`NewLeaderGate`](../lock/leader_gate.go) takes a `LeaderGateConfig` after the lock name, `Ttl`, at least 2ms like the exclusive run's, `RetryInterval`, `RefreshInterval`, `MaxConsecutiveRefreshFailures`, the new `MaxCampaignBackoff`, 1m when left out, and the three hooks; a `RefreshInterval` above half the ttl is refused naming the field and the ceiling, where the third major clamped it to half the ttl, and an unlimited threshold beside an unlimited ttl is refused naming both, a pair the third major spelled as a negative threshold under a 0 ttl and left with no signal to end a term. `NewExclusiveCommandWithName`, `NewLeaderGateWithOptions` and `LeaderGateOptions` are removed.

**Symptom.** `locker.CreateLock(name, 30*time.Second)`, `lock.Refresh(runtime, ttl)`, `RunExclusive(runtime, locker, name, ttl, callback)`, `NewExclusiveCommand(command, locker, ttl)`, `NewLeaderGate(locker, name, ttl)` and a locker or lock of the application's own declaring the `time.Duration` form no longer compile. A ttl of 0 handed to the in-memory locker, or any ttl under a millisecond, is refused at `Acquire`; an unlimited one is refused there by the in-memory and the redis lockers. A gate whose `RefreshInterval` exceeds half its ttl, or whose threshold and ttl are both unlimited, panics at construction. An exclusive run, command or gate with a ttl under 2ms is refused: the third major floored its renewal cadence at 1ms, so a lease of 1.5ms or less was demoted before its first renewal. A gate interval or threshold the third major read as its default when it was 0, and a `RefreshInterval` under 1ms it floored, are refused when set. An outbox `RelayConfig.LockTtl` under a millisecond is refused at `Acquire`, where the redis locker floored it to one.

**Remedy.** `bound.Of(ttl)` for a lease and `bound.UnlimitedDuration()` for the third major's 0 handed to a session locker; `lock.RunExclusive(runtime, locker, name, lock.ExclusiveConfig{Ttl: bound.Of(ttl)}, callback)`; `lock.NewExclusiveCommand(command, locker, lock.ExclusiveCommandConfig{LockName: name, Ttl: bound.Of(ttl)})` for `NewExclusiveCommandWithName`; `lock.NewLeaderGate(locker, name, lock.LeaderGateConfig{Ttl: bound.Of(ttl), ...})`, a positive interval or threshold wrapped in `bound.Of`, a 0 the third major read as the default left out, and a negative threshold spelled `bound.UnlimitedCount()`, beside a lease ttl only; a `RefreshInterval` the third major clamped is set to half the ttl or left out. A locker of the application's own takes the `bound.Duration` and reads it through `lockcontract.ResolveLeaseTtl` when it is a lease, or ignores it when it holds a session.

### Security: the list doors take slices, and the deprecated access control rule constructors are removed

**What changed.** [`NewAuthenticatorManager`](../security/authenticator_manager.go), [`NewAccessDecisionManager`](../security/access_decision_manager.go) after its strategy, [`NewAccessControl`](../security/access_control.go), [`accesscontrol.NewControl`](../security/accesscontrol/control.go), [`NewFirewall`](../security/firewall.go) and the builder's [`AccessControlBuilder.Require`](../security/config/access_control_builder.go) after its path prefix take their entries as a slice, where they took a variadic list; a nil entry is refused by its index as before, the caller's slice is copied, and a nil slice is the empty list, except on `Require`, whose rule still needs at least one attribute and refuses a nil, empty or blank list. `NewAccessDecisionManagerWithVoters` and the four deprecated `NewAccessControlRule`, `NewAccessControlRuleWithSegmentPrefix`, `NewAccessControlExactRule` and `NewAccessControlRegexRule` are removed; the `security.AccessControl` and `security.AccessControlRule` aliases stay.

**Symptom.** `security.NewAuthenticatorManager(a, b)`, `security.NewAccessDecisionManager(strategy, voter)`, `security.NewAccessControl(rule, ...)`, `NewFirewall(rule)`, `builder.Require(prefix, "ROLE_ADMIN")` and the four rule constructors no longer compile.

**Remedy.** `security.NewAuthenticatorManager([]securitycontract.Authenticator{a, b})`; `security.NewAccessDecisionManager(strategy, []securitycontract.Voter{voter}, security.AccessDecisionManagerConfig{})`, the slice handed as it was to `NewAccessDecisionManagerWithVoters`; `security.NewAccessControl([]security.AccessControlRule{...})`, `nil` for the empty control; `builder.Require(prefix, []string{"ROLE_ADMIN"})`; `accesscontrol.NewRawPrefixRule(path, accesscontrol.RuleConfig{Attributes: []string{...}})` for `NewAccessControlRule`, and `NewSegmentPrefixRule`, `NewExactRule` and `NewRegexRule` for the other three.

### Security: a value with one optional dependency is built through one door

**What changed.** [`NewActor`](../security/actor.go) takes the impersonator as `ActorConfig.Impersonator` and [`NewBearerTokenSource`](../security/bearer_token_source.go) the enricher as `BearerTokenSourceConfig.Enricher`, left out for none and a typed nil refused naming the field, and [`NewAuthenticatedToken`](../security/authenticated_token.go) the actor it carries as its third argument, nil for none and a typed nil refused naming the argument; [`NewImpersonationToken`](../security/impersonation_token.go) takes the role mode as its third, `RoleModeImpersonated` the zero value, and refuses an unknown mode, where the third major authorized such a token with the impersonated user's roles; [`JwtConfig`](../security/jwt_token_validator.go) gains `RevocationEpochStore`, under which a future `iat` is refused, a typed nil refused. A negative `JwtConfig.Leeway` is refused naming the field, as a negative `RevocationEpochSkew` was; the third major narrowed every time window by it. `NewActorWithImpersonator`, `NewAuthenticatedTokenWithActor`, `NewBearerTokenSourceWithEnricher`, `NewImpersonationTokenWithRoleMode` and `NewJwtTokenValidatorWithRevocationEpoch` are removed.

**Symptom.** `NewActor(identifier, type, roles, attributes)`, `NewAuthenticatedToken(user, roles)`, `NewBearerTokenSource(validator)`, `NewImpersonationToken(impersonated, impersonator)` and the five removed doors no longer compile; a validator built with a negative leeway panics.

**Remedy.** `security.NewActor(identifier, type, security.ActorConfig{Roles: roles, Attributes: attributes, Impersonator: impersonator})` and `security.NewBearerTokenSource(validator, security.BearerTokenSourceConfig{Enricher: enricher})`, a field left out for none; add `nil`, or the actor, as the last argument of `NewAuthenticatedToken`; `security.RoleModeImpersonated` for the two-argument impersonation token; `security.NewJwtTokenValidator(security.JwtConfig{..., RevocationEpochStore: store})` for `NewJwtTokenValidatorWithRevocationEpoch(config, store)`.

### Security: a token store's doors take the runtime and answer an error

**What changed.** Every write door of [`RevocableTokenStore`](../security/contract/token_store.go) and `RevocationEpochStore` takes the caller's runtime first and answers an error: `Put(runtimeInstance, token, claims, ttl) error`, `Delete(runtimeInstance, token) error`, `DeleteByUser(runtimeInstance, user) (int, error)` and `PurgeExpired(runtimeInstance) (int, error)`, the last two answering what they removed before a failure beside the error, and `RevokeBefore(runtimeInstance, user, device, instant) error`. Both framework stores refuse a nil runtime, a typed nil and one whose context is nil at every door, the reads included, through [`containercontract.RefuseNilRuntime`](../container/contract/runtime_refuse.go), naming `runtimeInstance`. A store that could not answer returns an error marked with `exception.MarkInfrastructureFailure`; a refused argument, an absent ttl, an empty user, a zero instant, a user that is not valid UTF-8, an instant outside 1970 to 2262, returns an unmarked one where both stores panicked. Both stores refuse a runtime whose context is already done at every door, marked, carrying the context's error, before any work. The redis store runs every round trip on the runtime's context capped by `TokenStoreConfig.CallTimeout`, so a write is cancelled with the request, names `TokenStoreConfig.CallTimeout` on a round trip that bound cut, and marks a failed `Lookup` or `RevocationEpoch` itself; `Delete` over an unreachable store returns its failure where it panicked.

**Symptom.** `not enough arguments in call to store.Put`; `assignment mismatch: 1 variable but store.DeleteByUser returns 2 values`; `*MyStore does not implement RevocableTokenStore (wrong type for method Delete)`. A door that panicked on a store outage or a refused argument now returns the error, which a caller that ignores it drops silently.

**Remedy.** Bind the store to the runtime and return or journal the error: [`security.TokenStoreFor(runtimeInstance, store)`](../security/bound_token_store.go) binds an `EpochRevocableTokenStore` a service holds, both framework stores being one, `security.TokenStoreFromRuntime(runtimeInstance, rueidis.ServiceTokenStore)` resolves and binds one the application registered under that name, and the view's doors take no runtime, `boundStore.Put(token, claims, ttl)`, `boundStore.RevokeBefore(user, device, instant)`, and a store of the application's own that implements only `RevocableTokenStore` is called through its contract, the runtime first; the wiring generator refuses a constructor argument of type `security.BoundTokenStore`. An implementation of the application's own takes the runtime first, marks a store failure with `exception.MarkInfrastructureFailure` and returns an argument refusal unmarked. A revocation or a withdraw that must outlive the request, a compensation after a committed write, binds the store again over a runtime detached from the request's cancellation, `security.TokenStoreFor(runtime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())), boundStore.TokenStore())`, the store's `CallTimeout` still bounding each round trip; the derived runtime keeps the request's scope, which closes with the request, so the call resolves its store before the request returns.

### Security: the in-memory token store refuses a token past `MaxEntries`

**What changed.** [`InMemoryTokenStoreConfig.MaxEntries`](../security/in_memory_token_store.go) bounds the tokens stored at once: 100,000 when left out, at least 1, `bound.UnlimitedCount()` for no ceiling. At the ceiling, after the lapsed and the revoked tokens are swept, a `Put` of a token the store does not hold is refused with `security.ErrTokenStoreFull` in its chain, marked as an infrastructure failure, so an issue door that returns it answers `503` with `Retry-After`; an overwrite of a held token and `RevokeBefore` are never refused.

**Symptom.** A `Put` answers `the token store is full`, naming `InMemoryTokenStoreConfig.MaxEntries`, after 100,000 stored tokens, where the store grew without end.

**Remedy.** Raise `MaxEntries`, or set `bound.UnlimitedCount()` for the behaviour of the third major.

### Security: `Claims` and `ActorData` marshal camelCase keys

**What changed.** [`Claims`](../security/contract/token_validator.go) marshals `userIdentifier`, `roles`, `issuedAt`, `deviceIdentifier`, `scope`, `attributes` and `originatingActor`, and [`ActorData`](../security/contract/actor.go) `identifier`, `type`, `roles`, `attributes` and `impersonator`, where both wrote the Go field names. The redis token store writes the claims so, and the internal-auth envelope carries its `actor` so; the envelope's signature covers the bytes, so the spelling does not touch verification. A Go reader decodes both spellings, `encoding/json` matching a key to a field case-insensitively, so a service on the third major and one on the fourth read each other's envelopes.

**Symptom.** A consumer outside Go that reads `actor.Identifier` from an internal-auth envelope, or `UserIdentifier` from a stored token, finds nothing.

**Remedy.** Read the camelCase key; a Go reader needs nothing.

### Rueidis: a token is stored under the digest of the token, and the tokens stored before the upgrade die

**What changed.** The redis token store keys a token by `{<prefix>}:token:<hex SHA-256 of the token>`, where it keyed it by the token itself, so the keyspace, the user's index set, `MONITOR`, the slow log and a backup hold no credential. A token must carry at least 128 bits of entropy: the digest is unkeyed, and a guessable token is found from its digest by whoever reads the keyspace. There is no migration and no read of the earlier key: a token stored before the upgrade is not found again. `DeleteByUser` and `PurgeExpired` still reach it through the user's index set, which lists its earlier key, and it lapses with its ttl.

**Symptom.** Every opaque bearer token issued before the upgrade is refused once, and its client signs in again; a monitoring script matching `…:token:<token>` finds nothing. A token stored with no expiry stays in redis, unreachable by `Lookup`, until `DeleteByUser` reclaims its user. During a rolling upgrade, replicas of the two majors sharing one prefix do not find each other's tokens, so a client flaps between `200` and `401` while both serve, and a third-major `Delete(token)` does not remove the fourth-major record: deploy every replica at once, or accept the flapping for the rollout. A rollback to the third major makes the tokens still stored under their verbatim keys live credentials again, and those the fourth major issued unknown: revoke the users whose sessions must not return before rolling back.

**Remedy.** None for the clients: they sign in again. A script reads the key as `hex(sha256(token))`. Run `DeleteByUser` for the users whose tokens were stored with no expiry to reclaim the earlier keys.

### Security: the HMAC secret provider and application registry take the runtime and answer an error

**What changed.** [`HmacSecretProvider`](../security/hmac_secret_provider.go) is `CurrentKeyId(runtimeInstance) (string, error)`, `Secret(runtimeInstance, keyId) ([]byte, bool, error)` and `ApplicationForKeyId(runtimeInstance, keyId) (string, bool, error)`, each taking the caller's runtime, and [`HmacApplicationRegistry`](../security/hmac_application_registry.go) `RolesForApplication(runtimeInstance, application) ([]string, bool, error)`. `false` still says the key id or the application is unknown, and the envelope is refused, anonymous; an error says the provider could not answer, and the verifier answers the unavailable token, `503` with `Retry-After` on a protected route. The verifier hands the provider and the registry the same request runtime. [`NewHmacEnvelopeSigner(runtimeInstance, secrets, application, config)`](../security/hmac_signer.go) takes the runtime it is built under, the provider and the application positionally, `HmacEnvelopeSignerConfig.Secrets` removed, and judges any provider's current key at construction under that runtime: a nil runtime, a key the provider cannot find, one shorter than `MinimumSecretLength`, one not bound to the signer's application, and a provider that cannot answer, panic. The signer is built at boot, inside a container provider registered `BuiltAtBoot`, under the runtime `resolver.Runtime()` answers. `Sign(runtimeInstance, method, path, body, actor)` refuses a nil runtime, hands the provider the runtime it received, judges the binding again whenever the current key id changes, and returns a provider failure marked with `exception.MarkInfrastructureFailure`. The static provider and the static registry refuse a nil runtime, and neither fails otherwise.

**Symptom.** `*MyProvider does not implement HmacSecretProvider (wrong type for method Secret)`; `not enough arguments in call to signer.Sign`; `unknown field Secrets in struct literal of type HmacEnvelopeSignerConfig`; `not enough arguments in call to security.NewHmacEnvelopeSigner`. A custom provider whose current key has no secret, or that cannot answer, now panics at construction where it signed nothing at `Sign`.

**Remedy.** Add the runtime and the error to a provider of the application's own, the runtime and the error to a registry of its own, returning an error only when the backend could not answer and `false` for an unknown key; build the signer once at boot, `security.NewHmacEnvelopeSigner(resolver.Runtime(), secrets, application, security.HmacEnvelopeSignerConfig{})` inside a container provider registered `RegisterOptions{BuiltAtBoot: true, BootFailure: container.BootFailureFailsBoot}` over a static provider, so its refusals fail the boot under the boot runtime, or with the default mode over a provider that reaches a secret store, so an outage warns and the first `Sign` builds it again; `signer.Sign(runtimeInstance, method, path, body, actor)`.

### Security: a token store's `Put` takes the ttl as a bound limit, and the in-memory store keeps a revocation boundary seven days

**What changed.** [`RevocableTokenStore.Put`](../security/contract/token_store.go) takes the ttl as a `bound.Duration` read against `securitycontract.TokenTtlRule()` through `securitycontract.ResolveTokenTtl`: at least 1s, `bound.UnlimitedDuration()` for a token kept until it is deleted or revoked, and an absent ttl or one under a second is refused naming `RevocableTokenStore.Put.ttl` on every implementation, returned as the error of `Put`; `PutWithTtl` is removed from the contract, the in-memory store and the redis store, whose own configuration is described in the Rueidis section on the locker, the nonce guard and the token store. [`NewInMemoryTokenStore`](../security/in_memory_token_store.go) takes an `InMemoryTokenStoreConfig`: `RevocationEpochRetention` is a `bound.Duration` of 7 days when left out, the redis store's default, at least 1s, `bound.UnlimitedDuration()` for boundaries kept until the process exits, the third major's default; `Clock` nil is the system clock, where the third major refused a nil one. `NewInMemoryTokenStoreWithClock` and the `WithRevocationEpochRetention` setter are removed.

**Symptom.** `store.Put(token, claims)`, `store.PutWithTtl(token, claims, ttl)`, `NewInMemoryTokenStore()` and a token store of the application's own declaring the third major's `Put` no longer compile. An in-memory store built without a retention drops a revocation boundary seven days after its instant, where the third major kept it forever; a ttl under a second is refused where `PutWithTtl` admitted it.

**Remedy.** `boundStore.Put(token, claims, bound.UnlimitedDuration())` for the third major's `Put`, `boundStore.Put(token, claims, bound.Of(ttl))` for `PutWithTtl`, over the store bound to the runtime by `security.TokenStoreFor`, or `store.Put(runtimeInstance, …)` on a store that implements only `RevocableTokenStore`, the error returned or journaled; `security.NewInMemoryTokenStore(security.InMemoryTokenStoreConfig{Clock: clock})`, `RevocationEpochRetention: bound.UnlimitedDuration()` where token lifetimes are not bounded. A store of the application's own reads the ttl through `securitycontract.ResolveTokenTtl`.

### Security: a nonce guard's ttl is a bound limit, and the in-process guard is built from one struct and bounded

**What changed.** [`NonceGuard.Remember`](../security/contract/nonce_guard.go) takes the ttl as a `bound.Duration` read against `securitycontract.NonceTtlRule()` through `securitycontract.ResolveNonceTtl`: at least 1ms, the residual lifetime of an envelope presented in its last second, rather than the 1s of a stored ttl; an absent ttl, one under a millisecond and an unlimited one are answered as the error, naming `NonceGuard.Remember.ttl`, where the third major's in-process guard answered a non-positive ttl as a fresh nonce without recording it and its redis guard answered it with a read-only existence check; the redis guard's own configuration is described in the Rueidis section on the locker, the nonce guard and the token store. The hmac source refuses an envelope whose residual lifetime is under a millisecond as too close to expiry, as it refused a non-positive one. [`NewMemoryNonceGuard`](../security/memory_nonce_guard.go) takes a `MemoryNonceGuardConfig`: `MaxEntries` a `bound.Count` of 100,000 when left out, at least 1, `bound.UnlimitedCount()` for the third major's unbounded map; at the ceiling, after the lapsed entries are swept, a new nonce is answered as the error, which the hmac source journals as an infrastructure failure and refuses the request on, never evicting a live nonce; `PurgeInterval` 1s when left out, at least 1ms, unlimited refused; `Clock` nil is the system clock, where the third major refused a nil one. `NewMemoryNonceGuardWithClock` is removed.

**Symptom.** `guard.Remember(runtime, nonce, ttl)` with a `time.Duration`, `NewMemoryNonceGuard()` and a guard of the application's own declaring the third major's `Remember` no longer compile. A guard holding 100,000 live nonces refuses the next one.

**Remedy.** `guard.Remember(runtime, nonce, bound.Of(ttl))`; `security.NewMemoryNonceGuard(security.MemoryNonceGuardConfig{Clock: clock})`, `MaxEntries: bound.UnlimitedCount()` for the third major's reading. A guard of the application's own reads the ttl through `securitycontract.ResolveNonceTtl` and answers its error.

### Security: the api key doors and the hmac key ring take structs, and the hmac limits are bound limits

**What changed.** [`NewApiKeyHeaderAuthenticator`](../security/api_key_authenticator.go) takes an `ApiKeyHeaderAuthenticatorConfig{HeaderName, ExpectedValue, UserIdentifier, Roles}` and [`NewApiKeyHeaderRule`](../security/rule.go) the matcher and an `ApiKeyHeaderRuleConfig{HeaderName, ExpectedValue}`, where they took three interchangeable strings; an empty required field is refused naming it. [`NewStaticHmacSecretProvider`](../security/hmac_secret_provider.go) takes an `HmacKeyRing{CurrentKeyId, Keys}`, its refusals naming the field. [`HmacEnvelopeSignerConfig.Ttl`](../security/hmac_signer.go) is a `bound.Duration` of 30s when left out, at least 1s, unlimited refused, where the third major read a non-positive ttl as the default. [`HmacTokenSourceConfig.MaxFutureExpiry`](../security/hmac_token_source.go) is a `bound.Duration` of 1h when left out, at least 1s, unlimited refused, where the third major's 0 left the horizon unbounded; a negative `Leeway` is refused naming the field. `HmacTokenSourceConfig.AcceptUntypedEnvelopes` is removed: an envelope without the internal-auth `typ` is always refused.

**Symptom.** `NewApiKeyHeaderAuthenticator(header, value, user, roles)`, `NewApiKeyHeaderRule(matcher, header, value)`, `NewStaticHmacSecretProvider(current, keys)`, `Ttl: ttl`, `MaxFutureExpiry: d` and `AcceptUntypedEnvelopes: true` no longer compile. A verifier with no `MaxFutureExpiry` refuses an envelope expiring more than an hour ahead; a signer ttl under a second panics.

**Remedy.** `security.NewApiKeyHeaderAuthenticator(security.ApiKeyHeaderAuthenticatorConfig{HeaderName: header, ExpectedValue: value, UserIdentifier: user, Roles: roles})`; `security.NewApiKeyHeaderRule(matcher, security.ApiKeyHeaderRuleConfig{HeaderName: header, ExpectedValue: value})`; `security.NewStaticHmacSecretProvider(security.HmacKeyRing{CurrentKeyId: current, Keys: keys})`; `Ttl: bound.Of(ttl)`, `MaxFutureExpiry: bound.Of(d)`, left out for the 1h horizon; roll every signer onto the typed envelope before moving the verifiers.

### Security: a compiled firewall is built from one struct

**What changed.** [`NewCompiledFirewall`](../security/compiled_configuration.go) takes a `CompiledFirewallConfig` with one field for each of its nineteen positional parameters, `Name`, `Matcher`, `MatcherDescription`, `Rules`, `TokenSource`, the five dependencies, the four login values and the five `Source` fields. An empty `Name` and a nil or typed-nil `Matcher` are refused at construction, with the messages the compile step gives; a `Source` left empty reads as `SourceNone`. `NewCompiledConfiguration` is unchanged.

**Symptom.** A firewall compiled by hand, a test fixture above all, no longer compiles; one that passed an empty name or a nil matcher panics.

**Remedy.** `security.NewCompiledFirewall(security.CompiledFirewallConfig{Name: name, Matcher: matcher, TokenSource: source, ...})`, every nil, empty string and `SourceNone` left out.

### Security: the builder declares the global configuration and a firewall in one struct each

**What changed.** [`SetGlobal`](../security/config/security_module.go) takes a `GlobalConfig{AccessControl, RoleHierarchy, AccessDecisionManager, EntryPoint, AccessDeniedHandler}`, a typed nil refused naming the field. [`AddFirewall`](../security/config/security_module.go) takes the name, the matcher and the token source, then a `FirewallConfig{Rules, Login, Logout, Override}`, where the third major chose a stateless or a stateful firewall by the door. `Override` is a `FirewallOverride{GlobalAccessControlDisabled, MergeStrategy, AccessControl, RoleHierarchy, AccessDecisionManager, EntryPoint, AccessDeniedHandler}` whose zero value inherits the global access control with the firewall's rules first; `GlobalAccessControlDisabled` inverts `WithInheritGlobalAccessControl(false)`, a `MergeStrategy` left empty is `AccessControlMergeLocalFirst`, an unknown one is refused at `AddFirewall` naming `FirewallConfig.Override.MergeStrategy`, and a typed nil among the override's dependencies is refused there naming its field, where the third major refused it only at `BuildAndCompile`. `AddStatelessFirewall`, `AddStatefulFirewall`, `FirewallOverrideConfiguration`, `NewFirewallOverrideConfiguration` and its eight `With` setters are removed, and so are the exported `GlobalConfiguration` and `FirewallConfiguration`, whose fields were unexported.

**Symptom.** `builder.SetGlobal(accessControl, nil, decisionManager, nil, nil)`, `AddStatelessFirewall(...)`, `AddFirewall` with nine arguments and `NewFirewallOverrideConfiguration().With...` no longer compile.

**Remedy.** `builder.SetGlobal(securityconfig.GlobalConfig{AccessControl: accessControl, AccessDecisionManager: decisionManager})`; `builder.AddFirewall(name, matcher, source, securityconfig.FirewallConfig{Override: securityconfig.FirewallOverride{EntryPoint: entryPoint}})` for `AddStatelessFirewall(name, matcher, nil, source, NewFirewallOverrideConfiguration().WithEntryPoint(entryPoint))`, and the `Login` and `Logout` blocks in the `FirewallConfig` for a stateful one; `GlobalAccessControlDisabled: true` for `WithInheritGlobalAccessControl(false)`. A firewall that called `WithStateless(true)` declares no door.

### Security: totp refuses what it clamped, and verifies on the config's clock

**What changed.** [`totp.Config`](../security/totp/totp.go) takes `Period` as a `bound.Duration`, 30s when left out, at least 1s and at most 24h, in whole seconds; `Digits` as a `bound.Count`, 6, from 6 to 8; `Skew` as a `bound.Count`, 1, from 0 to 10, where 0 now accepts the current step alone; unlimited is refused on all three, and a value outside its range is refused naming the field, where the third major clamped it to the default or the ceiling and read a skew of 0 as 1. `Config` gains `Clock`, nil the system clock, which `Verify` reads. `Config.Resolve` answers a `totp.Resolved` and an error; `Verify`, `GenerateCodeAt` and `OtpauthUri` answer the refusal as their error, `OtpauthUri` gaining an error return. `VerifyAt` and the deprecated `OtpauthURI` are removed. [`TotpSecondFactorAuthenticatorConfig`](../security/totp_second_factor_authenticator.go) refuses a `Totp.Clock`, naming `TotpSecondFactorAuthenticatorConfig.Totp.Clock`, since its own `Clock` is the clock the code is verified against, and refuses a `Totp` outside its ranges at construction.

**Symptom.** `totp.Config{Period: 30, Digits: 6, Skew: 1}`, `totp.VerifyAt(secret, code, at, config)`, `uri := totp.OtpauthUri(...)` and `resolved := config.Resolve()` no longer compile. A digit count, period or skew outside its range is answered as an error where it used to verify under the default.

**Remedy.** `totp.Config{Period: bound.Of(30 * time.Second), Digits: bound.Of(6), Skew: bound.Of(1)}`, or the zero value for those defaults; `totp.Verify(secret, code, totp.Config{Clock: clock})` for `VerifyAt`, `clock.NewFrozenClock(at)` for a fixed instant; take the error of `OtpauthUri` and `Resolve`; `OtpauthUri` for `OtpauthURI`.

### Security: a firewall no declared access control reaches is refused

**What changed.** `BuildAndCompile` refuses a firewall that neither declares an access control nor inherits one: under `AccessControlMergeOverrideOnly` or `GlobalAccessControlDisabled` without `FirewallOverride.AccessControl`, and under the default merge when no global access control is declared either. The refusal names the firewall and `FirewallConfig.Override.AccessControl`; [`Compile`](../security/config/compile.go) answers it as its error. The third major compiled an empty control for the first two, named them in the `security.firewallAccessControlEmpty` boot warning, which is removed, and compiled the third in silence; all three authorized every path they claimed for every token.

**Symptom.** The boot panics with "security firewall declares no access control and inherits none, so it would authorize every path it claims for every token", naming the firewall.

**Remedy.** Declare the rules the firewall enforces. Where the open firewall is meant, write `FirewallOverride{AccessControl: security.NewAccessControl(nil)}`, or declare a global control the firewall inherits; an explicit empty control is admitted at either place.

### Security: a firewall prefix no request path can carry is refused

**What changed.** [`NewPathPrefixMatcher`](../security/matcher.go) panics on a prefix that does not begin with a slash or carries an empty, `.` or `..` segment, naming `NewPathPrefixMatcher.prefix`: the kernel refuses a request path that is not canonical before any firewall is selected, so such a firewall was never selected. The string-prefix reading of a canonical prefix does not change; `""`, `/`, a trailing slash and a last segment that may still grow into a canonical one (`/api/.` claims `/api/.well-known`) are admitted as before. The third major named such a prefix in the `security.firewallPrefixUnreachable` boot warning, which is removed.

**Symptom.** `NewPathPrefixMatcher("api")` or `NewPathPrefixMatcher("/api//users")` panics with "firewall path prefix is never selected".

**Remedy.** Write the prefix canonical, beginning with a slash. A firewall over a tree is better served by the new `NewSegmentPrefixMatcher`, described below.

### Security: an HS256 or internal-auth secret shorter than 32 bytes is refused

**What changed.** [`NewJwtSecretKeySet`](../security/jwt_secret_key_set.go) refuses its `secret` argument, and [`NewStaticHmacSecretProvider`](../security/hmac_secret_provider.go) a key of `HmacKeyRing.Keys`, shorter than 32 bytes, the length of the hash RFC 7518 section 3.2 asks of an HMAC-SHA256 key, naming the argument or the field and the length, the key id for a ring, never the value. [`HmacTokenSource`](../security/hmac_token_source.go) judges the secret a custom `HmacSecretProvider` resolves for each envelope: a shorter one verifies nothing, the envelope answers anonymous and the misconfiguration is journaled at Error as an infrastructure failure naming the key id. [`NewHmacEnvelopeSigner`](../security/hmac_signer.go) refuses a short current key, and `Sign` refuses to sign with one a provider rotates to after boot, answering the refusal as its error. A new `MinimumSecretLength bound.Count` on `JwtSecretKeySetConfig`, `HmacKeyRing`, `HmacTokenSourceConfig` and `HmacEnvelopeSignerConfig` raises the floor: 32 when left out, at least 32, unlimited refused. The third major accepted a shorter secret and named it in the `security.jwtSecretShort` and `security.hmacSecretShort` boot warnings, which are removed.

**Symptom.** The boot panics with "jwt secret is shorter than its minimum", naming the argument `secret`, or "hmac secret is shorter than its minimum", naming the field, and the length; an internal caller whose custom provider hands a short secret is answered `503` with `Retry-After` on a protected route, with an Error record "security token source could not verify the credential: its platform is unavailable".

**Remedy.** Generate a secret of at least 32 random bytes (`openssl rand -base64 48`), and rotate the internal-auth key under a new key id so the overlap keeps the callers working.

### Security: `Leeway` and `RevocationEpochSkew` are bound limits, and the leeway is 5s by default

**What changed.** `JwtConfig.Leeway` and `HmacTokenSourceConfig.Leeway` are a `bound.Duration` of 5s when left out, where the third major's plain duration was 0, at least 0, at most 5m, unlimited refused. `JwtConfig.RevocationEpochSkew` is a `bound.Duration` of 0 when left out, at least 0, unlimited refused. The leeway widens the `exp`, `nbf` and `iat` checks and the HMAC envelope's window, and the nonce guard remembers a nonce for it too; it does not widen the revocation boundary or `MaxFutureExpiry`. Under a `JwtConfig.RevocationEpochStore` the future check of `iat` tolerates `RevocationEpochSkew` and the claim's one second in place of the leeway, since a token an ahead issuer stamped before a revocation would otherwise outlive it; the third major's `Leeway` widened that check, so a deployment that set one with a store sets the skew instead. This is a **Behavioural change** beside the type: a validator or source built without the field now tolerates 5s of clock skew.

**Symptom.** A `Leeway: 2 * time.Second` or `RevocationEpochSkew: 2 * time.Second` no longer compiles. A token past its expiry by 5s or less verifies, and an envelope past its expiry by under 5s; one up to 6s ahead of its `nbf` or issue time verifies, the 5s and the claim's one second, except that a token's issue time under a store takes the skew's reading above; a nonce stays in the guard 5s longer.

**Remedy.** Wrap a value in `bound.Of(2 * time.Second)`. To keep the third major's strict reading, set `Leeway: bound.Of(time.Duration(0))`: the constant needs its type, since `bound.Of(0)` is a count and does not compile here. An out-of-tree test measuring a time boundary with the zero value does the same.

### Security: the matched rule record carries the rule

**What changed.** [`NewMatchedAccessControlRule`](../security/matched_access_control_rule.go) takes the `accesscontrol.Rule` in place of its path prefix and attributes: `NewMatchedAccessControlRule(rule, source, ruleIndex, firewall)`.

**Symptom.** A call to the constructor with the five arguments no longer compiles. The framework's listener is its only caller; an application reading the record through its accessors is unaffected.

**Remedy.** None outside the framework: the constructor is unexported, see "Framework-only constructors left the surface".

### Bound: a rule may carry a maximum

**What changed.** [`bound.Rule`](../bound/resolve.go) gains `Maximum`; a zero `Maximum` sets no ceiling, and a value above a set one is refused as "the limit is above its maximum", naming the field, the value and the maximum.

**Symptom.** None for a rule written before: its `Maximum` is zero.

**Remedy.** None.

### Messagebus: the bus and the envelope take their lists as slices, and the handle middleware takes its options

**What changed.** [`NewManager`](../messagebus/manager.go) takes the middlewares as a `[]messagebuscontract.Middleware`, nil for none, and refuses a nil middleware by its index, where a nil one panicked at the first dispatch. [`NewEnvelope`](../messagebus/envelope.go) takes the stamps as a slice, nil for none, and copies it. The `messagebuscontract.Envelope` contract carries `WithStamps(stamps []Stamp)`; the `messagebuscontract.Bus` contract's `Dispatch` takes its stamps in a `messagebuscontract.DispatchOptions`. [`NewHandleMessageMiddleware`](../messagebus/middleware_handle.go) takes the `HandleMiddlewareConfig` and refuses a nil locator; `NewHandleMessageMiddlewareWithOptions` is removed.

**Symptom.** `NewManager("default", a, b)`, `NewEnvelope(message)`, `envelope.WithStamp(a, b)`, `bus.Dispatch(runtimeInstance, message)` and `NewHandleMessageMiddleware(locator)` no longer compile, and neither does a `Bus` or an `Envelope` implemented with the variadic signatures.

**Remedy.** `NewManager("default", []messagebuscontract.Middleware{a, b})`, `NewEnvelope(message, nil)`, `envelope.WithStamps([]messagebuscontract.Stamp{a, b})`, `bus.Dispatch(runtimeInstance, message, messagebuscontract.DispatchOptions{})`, `NewHandleMessageMiddleware(locator, messagebus.HandleMiddlewareConfig{})`; an implementation takes `options DispatchOptions` in `Dispatch` and adds `WithStamps`.

### Messagebus: the consume command is built from one struct, and the retry policy is bound limits whose zero value is the default

**What changed.** [`NewConsumeCommand`](../messagebus/consume_command.go) takes a `ConsumeCommandConfig{Retry, ShutdownTimeout}` and resolves the bus and the transports at run time (see the section on one constructor per command). [`RetryPolicy`](../messagebus/consume_command.go) takes `MaxRetries` and `MaxDeadLetterAttempts` as `bound.Count` and `Backoff` as a `bound.Backoff` and `FailureRequeueDelay` as a `bound.Duration`, each left out taking its default: `MaxRetries` 3, `bound.Of(0)` for no retry; `Backoff` 1s doubling to 1h, at least 1ms; `FailureRequeueDelay` 5s, now the delay of every requeue after the failure transport rejected an exhausted message, where the third major used the next backoff step whenever `BaseDelay` was positive; `MaxDeadLetterAttempts` 5, the message nacked without requeue at the fifth failed dead-letter routing, at least 1, `bound.UnlimitedCount()` to requeue until the failure transport recovers; `ShutdownTimeout` 30s. In the third major a zero `RetryPolicy` meant no retry, an immediate requeue and requeues for ever, so a container-registered policy naming only its `FailureTransport` disabled the retries. A value below its minimum or an unlimited one where the field does not admit it is refused naming the field, the failure transport is named by `FailureTransportName`, a padded name refused by name, and a policy the container still carries is read by nothing, named in the `messagebus.retryPolicyServiceIgnored` boot warning. `NewConsumeCommandWithRetry` and `WithShutdownGrace` are removed.

**Symptom.** `NewConsumeCommand(bus, transports)`, `NewConsumeCommandWithRetry(...)`, `NewConsumeCommandFromContainer()`, `.WithShutdownGrace(d)` and `RetryPolicy{MaxRetries: 2}` no longer compile. A `RetryPolicy{}` now retries three times with a 1s backoff and nacks an exhausted message without requeue at its fifth failed dead-letter routing.

**Remedy.** `NewConsumeCommand(messagebus.ConsumeCommandConfig{Retry: messagebus.RetryPolicy{MaxRetries: bound.Of(2)}, ShutdownTimeout: bound.Of(10 * time.Second)})`; write `MaxRetries: bound.Of(0)` or `MaxDeadLetterAttempts: bound.UnlimitedCount()` for the third major's zero-valued readings, and `Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}` for its immediate requeue.

### Messagebus: the in-memory transport is built from one struct, with a 64-slot queue by default

**What changed.** [`NewInMemoryTransport`](../messagebus/transport_in_memory.go) takes an `InMemoryTransportConfig{BufferSize, Logger}`: `BufferSize` a `bound.Count`, 64 when left out, 0 an unbuffered queue, a negative size or unlimited refused naming the field; `Logger` set once, a typed nil refused. `WithLogger` is removed.

**Symptom.** `NewInMemoryTransport(64)` and `.WithLogger(logger)` no longer compile.

**Remedy.** `NewInMemoryTransport(messagebus.InMemoryTransportConfig{BufferSize: bound.Of(64), Logger: logger})`, or the zero value for 64 slots.

### Mailer: the smtp timeouts are bound limits, and a value below 1ms is refused

**What changed.** [`SmtpConfig`](../mailer/smtp_transport.go) takes `DialTimeout`, `Timeout` and `DataTerminationTimeout` as `bound.Duration`: left out, `DialTimeout` is 30s, `Timeout` the resolved `DialTimeout`, `DataTerminationTimeout` four times the resolved `Timeout`, at least two minutes, as before; a value below 1ms or unlimited is refused at `NewSmtpTransport` naming the field, where a negative value silently took the default.

**Symptom.** `SmtpConfig{Timeout: 10 * time.Second}` no longer compiles; a negative timeout panics at construction.

**Remedy.** `SmtpConfig{Timeout: bound.Of(10 * time.Second)}`.

### Mailer: an smtp address that is empty or not `host:port` is refused

**What changed.** [`NewSmtpTransport`](../mailer/smtp_transport.go) panics on an empty `SmtpConfig.Address`, a padded one, one `net.SplitHostPort` cannot read with both a host and a port, and one whose port the dial cannot read, naming the field, before it judges the timeouts. The third major built the transport and failed at the first send's dial.

**Symptom.** The boot panics with "smtp address is empty", "smtp address is not host:port" or "smtp address names a port the dial cannot read"; an address of `:25` meaning the local host is refused.

**Remedy.** Write the address with its port, `smtp.example.com:587`, or wire no smtp transport where none is configured.

### Security: a firewall matcher bounded at a segment, and a configurable secret floor

**What changed.** [`NewSegmentPrefixMatcher(prefix)`](../security/matcher.go) is new: it claims the prefix and its tree under a `/` boundary, `/api` claiming `/api` and `/api/users` and never `/api-admin`, the reach of `accesscontrol.NewSegmentPrefixRule`; a trailing slash is folded, `/` claims every path, and an empty, relative or non-canonical prefix is refused naming the argument. The shadowed-firewall boot warning reads it at its segment boundary. `MinimumSecretLength` on `JwtSecretKeySetConfig`, `HmacKeyRing`, `HmacTokenSourceConfig` and `HmacEnvelopeSignerConfig` raises the 32-byte secret floor.

**Symptom.** None; an application that does not use them is unaffected.

**Remedy.** Declare a firewall over a tree with `NewSegmentPrefixMatcher` where a raw prefix claimed its siblings by accident; a raw `NewPathPrefixMatcher` keeps its string-prefix reading.

### Security: the matched rule record names a regex rule

**What changed.** [`MatchedAccessControlRule`](../security/matched_access_control_rule.go) gains `Rule()`, `Pattern()` and `Matching()`, so the record `SecurityContext.MatchedRule` answers for a regex rule names its pattern, where its `PathPrefix` was empty and only `RuleIndex` and `Source` pointed at it.

**Symptom.** None; an application that does not use them is unaffected.

**Remedy.** Where a regex rule was identified through `RuleIndex`, read `Pattern`.

### Security: the switch-user check reads the firewall's role hierarchy

**What changed.** This is a **Behavioural change**. `Compile` hands each firewall's effective hierarchy to a token source carrying the new optional [`securitycontract.RoleHierarchyBinder`](../security/contract/role_hierarchy.go), and `ImpersonationTokenSource` expands the admin's roles through it before the switch-role check when `ImpersonationTokenSourceConfig.RoleHierarchy` is nil, where the third major read the raw roles. The field still decides when it is set. A source held by firewalls with different hierarchies, a firewall without any included, is refused at `Compile` naming both firewalls.

**Symptom.** An admin holding `ROLE_ALLOWED_TO_SWITCH` only through the firewall's hierarchy can now switch user, where the switch was refused. A boot panics with "the impersonation source is held by two firewalls with different role hierarchies" where one source sits behind two firewalls that disagree.

**Remedy.** Drop a `RoleHierarchy` field that repeats the firewall's own hierarchy. Where the switch must stay on the raw roles, or one source serves firewalls that disagree, set the field to the hierarchy it should read, `security.NewRoleHierarchy(nil)` for none. A decorator around the source forwards `BindRoleHierarchy`.

### Event: a required listener skipped by a stop carries the stopper's failure through one door

**What changed.** [`NewRequiredListenerSkippedError`](../event/required_listener_skipped_error.go) takes the stopping listener's failure as a third argument, nil when it stopped cleanly; `NewRequiredListenerSkippedErrorWithStoppedListenerFailure` is removed. `NewRequiredListenerSkippedErrorWithCause` is unchanged.

**Symptom.** `NewRequiredListenerSkippedError(eventName, listenerName)` and the removed door no longer compile.

**Remedy.** None outside the framework: the constructor is unexported, see "Framework-only constructors left the surface".

### Logging: the json and default loggers are built from one struct each, an empty min level is info, and the depths are configurable

**What changed.** [`NewJsonLogger`](../logging/json_logger.go) takes the output and a `JsonLoggerConfig{MinLevel, Labels, Clock, CauseChainDepth, ContextNormalizationDepth}`: `MinLevel` info when left out, an unknown level still refused; `Labels` the default labels when nil; `Clock` the process clock when nil, a typed nil refused, where `NewJsonLoggerWithClock` refused a nil one; `CauseChainDepth` the links of an error's cause chain `LogError` writes, 8 when left out, at least 1, unlimited refused; `ContextNormalizationDepth` how deep a record's context is walked before the depth marker, 10000 when left out, from 1 to 10000. [`NewDefaultLogger`](../logging/default_logger.go) takes a `DefaultLoggerConfig{Labels, CauseChainDepth}`. A field outside its rule panics naming it. `LogError` reads the depth through the logger's `CauseChainDepth()`, which the request and process loggers forward to their base. `NewJsonLoggerWithLabels`, `NewJsonLoggerWithClock` and `NewDefaultLoggerWithLabels` are removed.

**Symptom.** `NewJsonLogger(output, level)`, `NewDefaultLogger()` and the three removed doors no longer compile.

**Remedy.** `NewJsonLogger(output, logging.JsonLoggerConfig{MinLevel: level, Labels: labels, Clock: clock})`, leaving out what took the default; `NewDefaultLogger(logging.DefaultLoggerConfig{})`.

### Logging: the exit handler and the shielded step take a budget struct, and the signal arming takes a slice

**What changed.** [`LogOnRecoverAndExit`](../logging/recover.go) takes an `ExitConfig{BeforeExit, BeforeExitBudget}` after the exit code: the hook nil for none, the budget a `bound.Duration`, 10s when left out, at least 1ms, unlimited refused since this path must reach `os.Exit`, checked like the exit code before the no-panic return, where a non-positive budget silently took 10s. [`RunShieldedStep`](../logging/recover.go) takes the step name, a `ShieldedStepConfig{Budget}` and the step: 10s when left out, at least 1ms, `bound.UnlimitedDuration()` waiting the step out where a non-positive budget did; a budget outside its rule is echoed on stderr naming the field and the step runs under 10s, since nothing on the exit path panics. [`ArmReopenOnSignal`](../logging/reopenable_file_writer.go) takes the signals as a slice, nil or empty still refused. `LogOnRecoverAndExitAfter` and `RunShieldedStepWithin` are removed.

**Symptom.** `LogOnRecoverAndExit(logger, recovered, 1)`, the two removed doors and `ArmReopenOnSignal(syscall.SIGHUP)` no longer compile; an `ExitConfig.BeforeExitBudget` set below 1ms panics at the call, while a `ShieldedStepConfig.Budget` set below 1ms is echoed on stderr and the step runs under 10s.

**Remedy.** `LogOnRecoverAndExit(logger, recovered, 1, logging.ExitConfig{BeforeExit: hook, BeforeExitBudget: bound.Of(budget)})`, the budget left out for 10s; `RunShieldedStep(name, logging.ShieldedStepConfig{Budget: bound.UnlimitedDuration()}, step)` where 0 meant no deadline; `ArmReopenOnSignal([]os.Signal{syscall.SIGHUP})`.

### Exception: `NewHttpError` takes a struct, the cause goes through it, and the `FromError` level doors fold into `FromErrorAs`

**What changed.** [`NewHttpError`](../exception/http_exception_new.go) takes the status and an `HttpErrorConfig{Code, Key, Message, Parameters, Cause, RetryAfter}`, every field optional, the zero value a bare exception of its status. `NewHttpExceptionWithCause` is removed: its cause goes through `NewHttpError`. [`FromErrorAs`](../exception/utility.go) takes the error and a `FromErrorConfig{Level, Context}`, the level error when left out and the context merged over the provider's; the zero config answers what `FromError` answers, an `*Error` unchanged. `FromErrorWithLevel` and `FromErrorWithLevelAndContext` are removed. `NewHttpException(status, message)` is unchanged.

**Symptom.** The six-argument `NewHttpError`, `NewHttpExceptionWithCause` and the two `FromError` level doors no longer compile.

**Remedy.** `NewHttpError(status, exception.HttpErrorConfig{Message: message, Cause: cause})`; `FromErrorAs(err, exception.FromErrorConfig{Level: level, Context: context})`.

### Exception: `LogContext` takes one struct, and the depth of its cause chain is configurable

**What changed.** [`LogContext`](../exception/utility.go) takes the error and a `LogContextConfig{Extra, CauseChainDepth}` in place of any number of extra maps: `Extra` merged on top of the error's context, its keys winning, nil adding nothing; `CauseChainDepth` the links of `causeChain` and of `causeContextChain`, 8 when left out as the hard-coded depth was, at least 1, a negative one refused with a panic naming the field before the error is read. A nil or typed-nil error answers a copy of `Extra`. `LogError` hands `LogContext` the logger's `CauseChainDepth`, so the json and default loggers' depth now reaches the chain of every error, not only of an `*exception.Error`; a depth below 1 reported by a foreign logger reads as one link.

**Symptom.** `LogContext(err)` and `LogContext(err, extra)` no longer compile; a caller that passed two or more extras merges them first.

**Remedy.** `LogContext(err, exception.LogContextConfig{})`; `LogContext(err, exception.LogContextConfig{Extra: extra})`.

### Validation: the validator is built from one struct whose limits are bound, the nesting depth and the map key length included

**What changed.** [`NewValidator`](../validation/validator.go) takes a `ValidatorConfig{MaxErrors, MaxRenderedPathLength, MaxAnswerBytes, MaxNestingDepth, MaxRenderedMapKeyLength}`, each a `bound.Count`: 1,000, 256 bytes, 512 KiB, 64 and 128 bytes when left out, each at least 1 and unlimited refused, the nesting depth at most 10,000; a field outside its rule panics naming it, where a zero field took the default. The nesting depth and the map key length, constants until now, are configurable. `NewValidatorWithLimits`, `ValidatorLimits` and `DefaultValidatorLimits` are removed.

**Symptom.** `NewValidator()` and the removed doors no longer compile; a zero limit panics at construction.

**Remedy.** `NewValidator(validation.ValidatorConfig{})`, or `ValidatorConfig{MaxErrors: bound.Of(50)}` for a limit of the application's choosing.

### Translation: `Translate` answers an error, and the manager takes one `ManagerConfig`

**What changed.** [`translationcontract.Translator`](../translation/contract/translator.go)'s `Trans(messageId, parameters, domain, locale) string` is `Translate(messageId string, config TranslateConfig) (string, error)`, the three optional values in `TranslateConfig{Parameters, Domain, Locale}`, and `HasMessage(messageId, config)` reads the same config's domain and locale. `Translate` tells three outcomes apart: the rendered message and nil; a message no catalog carries, the message id and an error matching [`translation.ErrMessageNotFound`](../translation/manager.go); a pattern that cannot be rendered whole, the empty string and its error naming the message id, the domain, the locale, the offset and the reason. [`translation.NewManager`](../translation/manager.go) takes one `ManagerConfig{Catalogs, FallbackLocales}`, its refusals naming the `field`; nil catalogs are a manager with no catalog, which answers every id with `ErrMessageNotFound`, and catalogs handed with an empty `FallbackLocales` are refused. `Loader.Load(ctx)` takes a context, and `JsonDirectoryLoader.Load` answers its error, wrapped with the directory, when it is done before the directory or before a file is read.

**Symptom.** `translator.Trans(…)`, `NewManager(catalogs, "en", fallbacks)` and `loader.Load()` no longer compile. A manager handed catalogs and no fallback locale panics at construction naming `ManagerConfig.FallbackLocales`.

**Remedy.** `message, translateErr := translator.Translate("greeting", translationcontract.TranslateConfig{Parameters: map[string]any{"name": name}, Locale: locale})`; where the third major printed the id of a missing message, keep `message` when `errors.Is(translateErr, translation.ErrMessageNotFound)` and treat any other error as a failure. `translation.NewManager(translation.ManagerConfig{Catalogs: []translationcontract.Catalog{english, romanian}, FallbackLocales: []string{configuration.Http().DefaultLocale()}})`, the list ending on the default locale, and `loader.Load(ctx)`.

### Container: the nil-runtime refusal names its door

**What changed.** [`containercontract.RefuseNilRuntime`](../container/contract/runtime_refuse.go) takes the door's name after the runtime and writes it under `door` in the refusal's context, so a journal line says which door was handed no runtime; the message `the runtime is nil or carries no context` is unchanged. Every framework and integration door names itself as `<package>.<Type>.<Method>`.

**Symptom.** A door of the application's own calling `containercontract.RefuseNilRuntime(runtimeInstance)` no longer compiles.

**Remedy.** `containercontract.RefuseNilRuntime(runtimeInstance, "catalog.Repository.Find")`, the door's own name.

### Container: a waiter builds again when the first caller left

**What changed.** A creation that fails while the context of the call that started it has ended is built again by a coalesced waiter whose own context is live, under the waiter's context and kind, where every waiter answered the first caller's failure (see [CONTAINER](package/CONTAINER.md)). A waiter whose own context ended, a waiter of a closed scope and a closed container answer their own refusal and run nothing.

**Symptom.** A provider runs a second time after its first caller hung up during its first run; a request that waited behind a cancelled one now answers the service where it answered "service creation failed".

**Remedy.** None for a provider that is safe to run again. A provider with a side effect that must not repeat after a failed run (a non-idempotent remote call) makes it idempotent or records it before failing.

### Storage: both backends refuse a nil runtime first

**What changed.** Every door of [`LocalStorage`](../storage/local.go) and of the [aws/s3 `Storage`](../../integrations/aws/s3/v4/storage.go) refuses a nil runtime, and one carrying no context, before anything else, answering `the runtime is nil or carries no context`: the local storage touches no file and the S3 backend sends no request. The local doors answered without a runtime before, and the S3 backend dereferenced it.

**Symptom.** A call such as `local.PresignedUrl(nil, key, config)` or `local.Get(nil, key, options)` answers the runtime refusal where it answered the door's own result.

**Remedy.** Hand the door the caller's runtime, or bind the storage once with `storage.FromRuntime(runtimeInstance, name)` and call the bound view.

### Messagebus: the failure transport is named, not handed over

**What changed.** [`RetryPolicy.FailureTransportName`](../messagebus/consume_command.go) names the registered transport an exhausted message is sent to, resolved from the transports when the consume command runs; `RetryPolicy.FailureTransport`, an instance built before the container, is removed. An empty name is none, a name with surrounding whitespace is refused by `NewConsumeCommand`, and a name no registered transport carries refuses the run, both naming `RetryPolicy.FailureTransportName`.

**Symptom.** `unknown field FailureTransport in struct literal of type messagebus.RetryPolicy`.

**Remedy.** Register the failure transport in `RegisterTransports` beside the others and write `RetryPolicy{FailureTransportName: "failed"}` with its name; the failure transport is then a container service, closed by the container's teardown.

### Openapi: a route is described through `Describe` alone, and a response is a value

**What changed.** [`Descriptor.Responses`](../openapi/registry.go) is a `[]ResponseDescription`, each entry built by `Response[T](status)`, an entry repeating a status replacing the earlier one, where it was a `map[int]reflect.Type`. `DescribeTyped`, `DescribeOption`, `WithSummary`, `WithDescription`, `WithTags` and `WithResponse` are removed: `Registry.Describe` is the one door, a typed route naming its request through `TypeOf[T]()`. The generated document is unchanged.

**Symptom.** `DescribeTyped[RequestBody, ResponseBody](registry, name, 201, openapi.WithSummary(...), openapi.WithTags(...))` and a `Responses: map[int]reflect.Type{…}` literal no longer compile.

**Remedy.** `registry.Describe(name, openapi.Descriptor{Summary: "...", Tags: []string{"..."}, RequestType: openapi.TypeOf[RequestBody](), Responses: []openapi.ResponseDescription{openapi.Response[ResponseBody](201), openapi.Response[NotFound](404)}})`; a response with no body is `openapi.ResponseDescription{Status: 204}`, documented with an `application/json` media type and an empty schema.

### Openapi: `SpecificationHandler` resolves the registry and the info from the container

**What changed.** [`SpecificationHandler()`](../openapi/spec_handler.go) takes no argument: at each request it resolves the `Registry` registered as `ServiceOpenApiRegistry` and the `Info` registered as `ServiceOpenApiInfo` from the request's scope, as `melody:openapi:generate` does. An absent info service serves an empty info block; an absent registry fails the request.

**Symptom.** `openapi.SpecificationHandler(info, registry)` no longer compiles.

**Remedy.** Register the registry and the info as the generate command already needs them, and mount `openapi.SpecificationHandler()`.

### Storage: the local storage declares its temp sweep in one struct

**What changed.** [`NewLocalStorage`](../storage/local.go) takes the base directory and a `LocalStorageConfig{TemporaryStaleAge, SweepBatchSize}`: the age past which a later `Put` sweeps a leftover temp object, 1h when left out, at least 1s, unlimited never sweeping, and the names the sweep reads at a time, 256 when left out, at least 1, unlimited refused; a field outside its rule panics naming it. Both were constants.

**Symptom.** `NewLocalStorage(baseDirectory)` no longer compiles.

**Remedy.** `NewLocalStorage(baseDirectory, storage.LocalStorageConfig{})`.

### Bunorm mysql / pgsql: a provider is built from one struct, its limits are bound limits, unlimited is typed, and an absent retry is the default retry

**What changed.** [`mysql.NewProvider`](../../integrations/bunorm/mysql/v4/provider.go) and [`pgsql.NewProvider`](../../integrations/bunorm/pgsql/v4/provider.go) take one `ProviderConfig{Pool, Timeout, Retry, PostBuildHook, Insecure, TlsConfig}`, resolved once: a limit outside its rule panics naming the field (`TimeoutConfig.ConnectTimeout`, `RetryConfig.Backoff.Factor`, ...) before any database is opened. The fields of `PoolConfig`, `TimeoutConfig` and `RetryConfig` are [`bound`](../bound/limit.go) limits, absent for the default, which keeps its value: pool 25 / 5 on mysql and 50 / 25 on pgsql, lifetime 5m and idle time 1m, at least 1s; connect 10s on mysql and 5s on pgsql, read and write 30s, at least 1ms; retry 3 attempts, 500ms, 5s, at least 1 and 1ms. Unlimited is `bound.UnlimitedCount()` / `bound.UnlimitedDuration()` where a negative value was, admitted on the pool and on the read and write deadlines, refused on the connect timeout, which bounds the dial, the hook and the boot ping, and on every retry field. `MaxIdleConnections: bound.Of(0)` keeps no idle connection, which a zero could not express. `Backoff.Factor` stays a float, 0 the default 2, below 1 or NaN refused where it fell back to 2; a delay under 1ms is refused where it was raised to 1ms. A left-out `RetryConfig` is the default retry, three attempts, where an absent one meant a single attempt; and a single attempt journals its terminal failure and marks it logged, as the last attempt of a retry always did, where it answered the failure unrecorded. An empty `ConnectionParameters.Port` dials the driver's default, 3306 or 5432, where it dialled `host:`. `ProviderOption` with its six `With*` options, `DefaultPoolConfig`, `NewPoolConfig`, `DefaultTimeoutConfig`, `NewTimeoutConfig`, `DefaultRetryConfig`, `NewRetryConfig` and `Unlimited` are removed.

**Symptom.** `NewProvider(WithRetryConfig(NewRetryConfig(3, time.Second, 5*time.Second, 2.0)), WithInsecure(true))` no longer compiles. A provider built with no retry retries a transient failure three times, the backoff included, before it answers.

**Remedy.** `NewProvider(mysql.ProviderConfig{Retry: mysql.RetryConfig{MaxAttempts: bound.Of(3), Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}}, Insecure: true})`; a zero field becomes a left-out one, `Unlimited` or a negative value `bound.UnlimitedCount()` / `bound.UnlimitedDuration()`, and an unlimited connect timeout a bounded one. Where one attempt is wanted, write `Retry: mysql.RetryConfig{MaxAttempts: bound.Of(1)}`.

### Bunorm mysql / pgsql: the locker is built from one struct, and its release honours the caller's earlier deadline

**What changed.** [`mysql.NewLocker`](../../integrations/bunorm/mysql/v4/lock.go) and [`pgsql.NewLocker`](../../integrations/bunorm/pgsql/v4/lock.go) take the database and a `LockerConfig{ReleaseTimeout}`, 5s when left out, at least 1ms, unlimited refused, a value outside the rule panicking naming `LockerConfig.ReleaseTimeout` where a non-positive one took 5s. `RegisterLockerService` takes the config after the database and resolves it at the call, so a refusal names the wiring, not the first lock, and `ModuleConfig.Locker` hands it from the module. `Release` runs on the runtime's context without its cancellation, so a cancelled request context still releases, and ends at the earlier of the locker's budget and the runtime's own deadline while that one is still ahead, a deadline already passed read as the cancellation is: a `RunExclusive` or `LeaderGate` release budget shorter than the locker's is honoured, where the locker released on a detached budget of its own and a caller's 1s budget was a 5s stall. A release cut by either deadline ends the session, which frees the lock server-side, and its failure names `releaseTimeout` and `deadline`. `LockerOption` and `WithLockReleaseTimeout` are removed.

**Symptom.** `NewLocker(database)` and `NewLocker(database, WithLockReleaseTimeout(2*time.Second))` no longer compile.

**Remedy.** `NewLocker(database, mysql.LockerConfig{})` and `NewLocker(database, mysql.LockerConfig{ReleaseTimeout: bound.Of(2 * time.Second)})`, the same on pgsql; the module registers the locker from `ModuleConfig{Database: database, Locker: &mysql.LockerConfig{}}`.

### Bunorm: the registry is built from one door, the definitions as a slice

**What changed.** [`NewManagerRegistry`](../../integrations/bunorm/v4/manager_registry.go) takes the logger and the provider definitions as a `[]ProviderDefinition`; it was the door with variadic definitions, beside `NewManagerRegistryWithContext`, which is removed. The refusals are the ones the two doors gave, now joined by the two of the next section.

**Symptom.** `NewManagerRegistry(logger, primary, replica)` and `NewManagerRegistryWithContext(ctx, logger, definitions...)` no longer compile.

**Remedy.** `NewManagerRegistry([]bunorm.ProviderDefinition{primary, replica}, bunorm.ManagerRegistryConfig{Logger: logger})`; the context an open runs under is the caller's, handed to each door through its runtime.

### Bunorm: the connection parameters are one exported struct, refused by name when the host or the database is empty, and the deprecated alias is gone

**What changed.** [`bunorm.ConnectionParameters`](../../integrations/bunorm/v4/connection_parameters.go) is the one shape of a connection's values. `NewManagerRegistry` refuses a definition whose `Parameters.Host` or `Parameters.Database` is empty, with `ErrConnectionHostIsRequired` or `ErrConnectionDatabaseIsRequired` as the cause and the definition's position, its name and the field, `ProviderDefinition.Parameters.Host` or `ProviderDefinition.Parameters.Database`, in the context; an empty host reached the provider and was refused at the first open, an empty database was refused there by pgsql alone and dialled by mysql. `User` and `Password` stay free, since a socket or a trust-authenticated local database has neither, and an empty `Port` dials the driver's default, 3306 or 5432. `mysql.ConnectionConfig` and `pgsql.ConnectionConfig`, with `NewConnectionConfig` and their five getters, are removed: the providers read the `ConnectionParameters` the registry hands their open, and the failed-open errors carry its `SafeContext`, the password left out, as before; the pool and the deadlines in those errors are named as the record reads them, `unlimited` or the value. The deprecated alias `ConnectionParams` is removed.

**Symptom.** A call to `NewConnectionConfig` and a use of `ConnectionParams` no longer compile; an application never needed the first to open a database. A registry built with a definition whose host or database is empty answers an error where it built.

**Remedy.** Read the values from the `ConnectionParameters` you hand the registry, write `ConnectionParameters` where `ConnectionParams` was, and set the host and the database on every definition.

### Bunorm: the read/write splitter is built from one struct, and the replica retry interval is a bound limit

**What changed.** [`NewReadWriteSplitter`](../../integrations/bunorm/v4/split.go) takes the registry, the primary's name and a `SplitterConfig{ReplicaNames, ReplicaRetryInterval}`: the replicas in the order the reads rotate over, none routing every read to the primary, an empty name refused by its index; and the time an unreachable replica is skipped, a [`bound.Duration`](../bound/limit.go), 5s when left out, at least 1ms, unlimited refused, a value outside the rule panicking naming `SplitterConfig.ReplicaRetryInterval`, where a zero interval took the default and a negative one panicked. `NewReadWriteSplitterWithOptions`, `ReadWriteSplitterOption` and `WithReplicaRetryInterval` are removed.

**Symptom.** `NewReadWriteSplitter(registry, "primary", "replica-1", "replica-2")` and `NewReadWriteSplitterWithOptions(registry, "primary", replicas, WithReplicaRetryInterval(30*time.Second))` no longer compile.

**Remedy.** `NewReadWriteSplitter(registry, "primary", bunorm.SplitterConfig{ReplicaNames: []string{"replica-1", "replica-2"}, ReplicaRetryInterval: bound.Of(30 * time.Second)})`, the interval left out for the default.

### Bunorm migrate: a migration prints through one door per verb, its zero option follows the command, the process-wide runner option is gone, and the unlock budget is an option

**What changed.** [`RunQueries`](../../integrations/bunorm/migrate/v4/migrate.go), `Up` and `Down` take a `RunnerOption` last, and `RunQueriesWithOption`, `UpWithOption` and `DownWithOption` are removed. `RunnerOption{}` prints as the running command prints: a nil `Writer` takes the writer the command put on the context, else standard output, and `NoColor` false follows the command's colour, where an explicit zero option through the `WithOption` doors forced colour under `--no-color`; `NoColor: true` still prints plain. `DefaultRunnerOption` and `SetDefaultRunnerOption` are removed with the process-wide fallback they fed, which `db:migrate` and `db:rollback` installed for the length of their run: the context the migrator hands every migration is the one channel from a command to its migrations. `DefaultOptions()` is removed; `Options{}` takes every default at `RegisterCommands` and, now, at every command constructor too, and the three defaults are the constants `DefaultManagerRegistryServiceId`, `DefaultManagerFlagName` and `DefaultCommandPrefix`. `Options.UnlockTimeout`, a `bound.Duration`, bounds the release of the migration lock after a run, 5s when left out as the constant was, at least 1ms, unlimited refused, a value outside the rule panicking at registration naming `Options.UnlockTimeout`; a migration context inherits the base options' value unless its own options set one.

**Symptom.** `RunQueries(ctx, db, "up", name, queries)`, `Up(ctx, db, name, queries)`, the three `WithOption` doors, `DefaultRunnerOption()`, `SetDefaultRunnerOption(...)` and `DefaultOptions()` no longer compile. A migration that drops its context and calls `RunQueries` on `context.Background()` prints to standard output under every command posture, `--format=json` included, where the command's fallback kept those lines out of the document.

**Remedy.** Add `migrate.RunnerOption{}` as the last argument, or the option the migration passed to the `WithOption` door; pass the migration's context on, as the generated skeleton does. Read `DefaultManagerRegistryServiceId` where `DefaultOptions().ManagerRegistryServiceId` was, and write `Options{}` where `DefaultOptions()` was handed whole. A host that ran migrations outside the commands puts its posture in the `RunnerOption` it passes.

### Bunorm audit: the registry, the recorder and the async storage are built from one struct each, and the storage contract takes its entries as a slice

**What changed.** [`audit.NewRegistry`](../../integrations/bunorm/v4/audit/config.go) takes a `RegistryConfig{DefaultTable, GlobalIgnoredFields}`, the table `DefaultTable` when left out and refused naming `RegistryConfig.DefaultTable` when it is not a plain SQL identifier. [`NewRecorder`](../../integrations/bunorm/v4/audit/recorder.go) takes the storage and a `RecorderConfig{Registry, OwnsStorage, Logger}`: a nil registry is a registry over `DefaultTable`, `OwnsStorage` makes `Close` close the storage, and the dead-letter logger is set once, none when left out as before, a typed nil refused naming `RecorderConfig.Logger`; the `(database, table)` door, `NewRecorderWithStorage`, `NewRecorderOwningStorage` and `Recorder.WithLogger` are removed. [`NewAsyncStorage`](../../integrations/bunorm/v4/audit/async_storage.go) takes the delegate and an `AsyncStorageConfig{BufferSize, Logger}`: `BufferSize` a `bound.Count`, 1024 when left out, at least 1, unlimited refused, where a non-positive size took 1024; `Logger` the dead-letter logger, the emergency logger when nil, a typed nil refused; a field outside its rule panics naming it. `AsyncStorage.WithLogger` is removed. The `Storage` contract's `Save` takes the entries as a `[]Entry`, where it was variadic; a nil or empty slice is a no-op.

**Symptom.** `NewRegistry("melody_audit", "updated_at")`, `NewRecorder(database, table)`, `NewRecorderWithStorage(storage, registry)`, `NewRecorderOwningStorage(...)`, `NewAsyncStorage(delegate, 1024)` and `.WithLogger(logger)` no longer compile; a storage of your own fails with `does not implement audit.Storage (wrong type for method Save)`, and a call `storage.Save(ctx, table, entry)` no longer compiles.

**Remedy.** `NewRegistry(audit.RegistryConfig{DefaultTable: "melody_audit", GlobalIgnoredFields: []string{"updated_at"}})`; `NewRecorder(audit.NewBunStorage(database), audit.RecorderConfig{Registry: registry, Logger: logger})`, `OwnsStorage: true` where the owning door was; `NewAsyncStorage(delegate, audit.AsyncStorageConfig{Logger: logger})`, a size or a grace only where one is wanted; `Save(ctx context.Context, table string, entries []Entry) error` on your storage, and `storage.Save(ctx, table, []audit.Entry{entry})` at a call.

### Bunorm audit: the schema door takes the runtime and runs at boot

**What changed.** [`Registry.EnsureSchema`](../../integrations/bunorm/v4/audit/config.go) takes the runtime first, `EnsureSchema(runtimeInstance, database)`, its statements run under the runtime's context, and a nil runtime, a typed nil and one carrying no context are refused with `the runtime is nil or carries no context` before any statement. The third major took a context.

**Symptom.** `cannot use ctx (variable of interface type context.Context) as containercontract.Runtime value in argument to registry.EnsureSchema`.

**Remedy.** Call it from a service registered with `container.RegisterOptions{BuiltAtBoot: true}` whose provider hands it `resolver.Runtime()`, resolved by what writes the trail: an http process ensures the schema at boot, and when the database does not answer it boots with the `application.service.builtAtBootFailed` warning and the first audited write builds the service again under its own runtime; a command that empties or recreates the trail hands it its own run. A constructor that ensured the schema on a `context.Background()` drops the call: a constructor holds no runtime, and the first caller's runtime is a request that may be cancelled.

### Bunorm encrypt: the bulk command is built from one door, the cipher is installed by the module, and the key ring is a struct

**What changed.** [`UseCipher`](../../integrations/bunorm/v4/encrypt/cipher_registry.go) and `UseCipherNamed` are removed: the [encrypt module](../../integrations/bunorm/v4/encrypt/module.go) is a `ServiceModule`, and `RegisterServices` installs `ModuleConfig.Cipher` for `EncryptedString` and `EncryptedDeterministicString` and each `CommandContextConfig.Cipher` under the context's `Name` for the columns bound through a `CipherReference`, in every process mode, and publishes the default cipher as `encrypt.ServiceCipher`, and an application registers the module once, as every module; a later application booted in the process replaces the whole cipher and binding tables of the earlier one. `ModuleConfig{Cipher}` with no database is legal and registers no command, and so is a context with a cipher and no database; a module with no `Cipher` and no `Contexts`, which registered nothing, is refused, and so are a repeated context name and a prebuilt database not of the MySQL dialect, judged before any cipher is installed; every refusal names the field. [`NewEncryptDatabaseCommand`](../../integrations/bunorm/v4/encrypt/encrypt_database_command.go) takes the cipher and an `EncryptDatabaseCommandConfig{Database, DatabaseResolver, CommandName}`, exactly one of the two database fields, both or neither refused naming them, `melody:encrypt:database` when `CommandName` is left out; `NewEncryptDatabaseCommandWithName`, `NewEncryptDatabaseCommandFromResolver` and `NewEncryptDatabaseCommandFromResolverWithName` are removed, [`Commands`](../../integrations/bunorm/v4/encrypt/command.go) takes the same cipher and config and `CommandsFromResolver` is removed. The deprecated `GoString` of `EncryptedString`, `EncryptedDeterministicString`, `EncryptedStringFor`, `EncryptedDeterministicStringFor` and `StaticKeyProvider` is removed: it was never reached, `Format` answers `%#v` and keeps the redaction. [`NewStaticKeyProvider`](../../integrations/bunorm/v4/encrypt/key_provider.go) takes a `KeyRing{CurrentKeyId, Keys}`, its refusals unchanged but naming `KeyRing.CurrentKeyId` or `KeyRing.Keys`. [`TableSpecification.BatchSize`](../../integrations/bunorm/v4/encrypt/migrate.go) is a `bound.Count`, 500 when left out, at least 1, unlimited refused, a run outside the rule refused before its first query naming the field, where a zero or negative size took 500; `--batch 0` still selects the default, and `--batch -1` is refused naming the flag.

**Symptom.** `undefined: encrypt.UseCipher`, `undefined: encrypt.UseCipherNamed`, `undefined: encrypt.NewEncryptDatabaseCommandFromResolver` and `undefined: encrypt.CommandsFromResolver`; a call of `.GoString()` on a column or a key provider no longer compiles; `NewStaticKeyProvider("v1", keys)`, `NewEncryptDatabaseCommand(database, cipher)`, `Commands(database, cipher)` and `TableSpec{BatchSize: 100}` no longer compile; `encrypt.NewModule(encrypt.ModuleConfig{})` panics at boot; a process that installed its cipher with `UseCipher` and never registered the module answers `no cipher is installed; register the encrypt module with a Cipher` at the first encrypted column.

**Remedy.** `encrypt.NewStaticKeyProvider(encrypt.KeyRing{CurrentKeyId: "v1", Keys: keys})`; drop `UseCipher(cipher)` and register `encrypt.NewModule(encrypt.ModuleConfig{Cipher: cipher})` in every process that reads an encrypted column, its `Database` or `DatabaseFactory` where the bulk command is wanted; a `UseCipherNamed(name, cipher)` becomes a `CommandContextConfig{Name: name, Cipher: cipher}` in `ModuleConfig.Contexts`; a test that installed a cipher registers the module through a registrar; `NewEncryptDatabaseCommand(cipher, encrypt.EncryptDatabaseCommandConfig{Database: database})`, `DatabaseResolver: resolver` in place of the resolver doors and `CommandName: name` in place of the named ones, `Commands(cipher, config)` alike; `fmt.Sprintf("%#v", value)` in place of `.GoString()`; `TableSpecification{BatchSize: bound.Of(100)}`, the field left out for the default.

### Bunorm encrypt: a stored value under an unknown seal format is refused, and a seal can be bound to its column

**What changed.** A stored value that begins with the encryption marker and its glue, `<ENC>\0`, under a format this build does not read is an error on the package's [`Cipher.Decrypt`](../../integrations/bunorm/v4/encrypt/cipher.go), on the `Scan` of the four column types over it and in every mode of the [`Migrator`](../../integrations/bunorm/v4/encrypt/migrate.go) whatever its cipher, the width check before the rows leaving such a value out, [`ErrSealFormatUnknown`](../../integrations/bunorm/v4/encrypt/errors.go) in its chain, the format named only when it is a lower-case token of at most 16 characters, never the key id or the body. Such a value was read as plaintext and passed through, and the next write or `encrypt` run sealed it again as data, so a seal of a newer build, or a damaged marker, lost its content without an error. Through the package cipher the write side is unchanged: a value an application writes in that shape is sealed as data and reads back byte for byte. Through a cipher implemented outside the package, a column's `Value` and the `Migrator`'s re-seal refuse a value of the family, as they refused a `gcm1` one.

**Symptom.** `encrypted value is sealed under a format this build does not read` on a read of a column or a bulk run, the run stopping at the row it names with no update: a value written by a newer build, or damage behind the marker. Through a wrapped or foreign cipher, a write of such a value is refused as carrying the encryption marker.

**Remedy.** Read and migrate such a column with the build that wrote it, or restore the damaged rows; a value that begins with `<ENC>` without the NUL glue, as bun's formatter leaves one inlined as a `string`, is still plaintext.

**Binding, opt-in.** A marker implementing [`SealBindingReference`](../../integrations/bunorm/v4/encrypt/cipher_registry.go), declared in `ModuleConfig.SealBindings`, makes its `…For` columns write `gcm2`, a seal that opens through a column of that marker alone. `Cipher.Decrypt`, `EncryptedString`, `EncryptedDeterministicString` and a `…For` column of an unbound marker refuse such a seal with [`ErrSealBound`](../../integrations/bunorm/v4/encrypt/errors.go): `encrypted value is bound to a column and cannot be opened without its binding` is a bound seal read through a plain column type. A binary that predates the binding, the third major's included, reads a `gcm2` seal as text, so bind a column only once the roll-back target is this major. A bound column refuses `gcm1` seals and unmarked values; `SealBindingConfig.UnboundAdmitted` is the door that admits them while the column is migrated. To bind a column that holds values: deploy the marker and its declaration with `UnboundAdmitted: true`; run `melody:encrypt:database --table=<table> --column=<column> --mode=encrypt`, which reads the declaration by the column's `<table>.<column>`, or by `--seal-binding column=label` for a table spelled otherwise, seals each value without the marker under the binding and re-seals each `gcm1` seal under it with its key and mode kept; then deploy the declaration without `UnboundAdmitted`. A [`TableSpecification`](../../integrations/bunorm/v4/encrypt/migrate.go) a programmatic run is handed binds a column through `TableSpecification.SealBindings`; a column it leaves unbound stops at the first `gcm2` row with `ErrSealBound`.

### Websocket: the module declares its streams, one module per application

**What changed.** [`ModuleConfig`](../../integrations/websocket/v4/module.go) declares its routes as `Streams []StreamConfig`, each [`StreamConfig`](../../integrations/websocket/v4/module.go) carrying the `Hub` provider, `Handler`, `RouteName` and `Path`, `Handler` being the `StreamHandlerConfig` the module carried as `Options`. A module with no stream, a stream with a nil hub provider or an empty path and, on a module declaring more than one stream, an empty `RouteName` are refused at boot, naming the field and the index; every stream is judged and its handler built before the first route registers, so a refused module leaves no route behind. The default route name `melody.websocket` names a single stream.

**Symptom.** `melodywebsocket.ModuleConfig{Hub: …, Path: …}` does not compile. Two websocket modules, the way two endpoints were registered, fail the boot as a duplicate module.

**Remedy.** `ModuleConfig{Hub: hub, Path: "/ws", Options: options}` is `ModuleConfig{Streams: []StreamConfig{{Hub: hubProvider, Path: "/ws", Handler: config}}}`, `config` being a `StreamHandlerConfig` and `hubProvider` the hub's provider; two endpoints are two entries of `Streams`, each with its own `RouteName`.

### Bunorm encrypt: a registration installs the whole cipher and binding tables

**What changed.** The encrypt module installs its ciphers, the default one and one per context, and its seal bindings by replacing the whole process-wide tables when it registers its services, where it replaced the entries it named and kept the others. An application registers the module once, as every module, so every compartment and every bound column is declared on one `ModuleConfig`.

**Symptom.** In a process that boots several applications, a test suite among them, an application no longer reads a compartment or a binding only an earlier application declared: a column of such a compartment answers that no cipher is installed under its name, and a column of such a marker refuses its bound seals as undeclared.

**Remedy.** Declare on each application's module every compartment and every binding its columns read.

### Rueidis: the provider is built from one struct whose zero value is the default, an absent retry is the default retry, and the library's own deadlines never govern a dial

**What changed.** [`NewProvider`](../../integrations/rueidis/v4/provider.go) takes one [`ProviderConfig`](../../integrations/rueidis/v4/provider_config.go) of `Client`, `Retry` and `Logger`, resolved once, a limit outside its rule panicking naming the field before any client is built. [`ClientConfig`](../../integrations/rueidis/v4/client_config.go)'s zero value is the default: `BootPingDisabled` and `ClientSideCacheEnabled` replace `PingOnStart` and `DisableCache`, so a partial literal keeps the boot ping and leaves the client-side cache off, where the third major's partial literal turned both the other way; the new `ClientSideCacheBytes` bounds the client-side cache of each connection, 128 MiB when left out, refused naming both fields when the cache is not enabled. `DialTimeout` and `ConnectionWriteTimeout` are `bound.Duration`s, 5s when left out, at least 1ms, unlimited refused, and are always installed, so the library's own five-second dial and ten-second write defaults, which a zero or negative value used to select, never govern a melody client. `BootPingTimeout`, the boot ping's deadline, is a `bound.Duration`, 3s when left out, at least 1ms, unlimited refused. [`RetryConfig`](../../integrations/rueidis/v4/retry_config.go) takes the bunorm twins' form: `MaxAttempts` 3, at least 1, `Backoff.Initial` 500ms and `Backoff.Max` 5s, at least 1ms, unlimited refused on all three, and a `Backoff.Factor` below 1 or NaN refused where it fell back to 2. An absent `RetryConfig` is the default retry, where it made a single attempt. `ProviderOption` and its four options, `DefaultClientConfig`, `DefaultTimeoutConfig`, `DefaultRetryConfig`, `NewRetryConfig`, `NewConnectionParameters`, the deprecated `NewConnectionParams` and the deprecated `ConnectionParams` alias are removed.

**Symptom.** `NewProvider()`, `NewProvider(WithRetryConfig(...))`, `DefaultClientConfig()`, `&ClientConfig{PingOnStart: true, ...}` and `NewConnectionParameters(address, user, password)` no longer compile. A provider built without a retry dials a dead address three times, with the default backoff, before it refuses, and journals the attempts; a `ClientConfig` literal that set only a name now pings on start and keeps the client-side cache off; a deadline set to `bound.Of(0)`, a negative value or one under a millisecond panics at `NewProvider`, while a field left at its zero value takes the default.

**Remedy.** `rueidis.NewProvider(rueidis.ProviderConfig{})`; `Retry: rueidis.RetryConfig{MaxAttempts: bound.Of(10), Backoff: bound.Backoff{Initial: bound.Of(time.Second)}}` for a retry of your own and `MaxAttempts: bound.Of(1)` for the third major's single attempt; `Client: rueidis.ClientConfig{ClientName: "app"}`, `BootPingDisabled: true` where the ping was off and `ClientSideCacheEnabled: true` where the cache was on; `Logger: logger` for `WithLogger(logger)`; `provider.Open(runtimeInstance, rueidis.ConnectionParameters{Address: address, User: user, Password: password})`.

### Rueidis: the locker, the nonce guard and the token store are built from one struct each, their call budgets are bound limits, and a registration resolves at the call

**What changed.** [`NewLocker`](../../integrations/rueidis/v4/lock.go) takes a `LockerConfig` of `CallTimeout`; [`NewNonceGuard`](../../integrations/rueidis/v4/nonce_guard.go) a `NonceGuardConfig` of `KeyPrefix` and `CallTimeout`; [`NewTokenStore`](../../integrations/rueidis/v4/token_store.go) a `TokenStoreConfig` of `Prefix`, `ScanCount`, `CallTimeout`, `Clock`, `MaximumClockSkew` and `RevocationEpochRetention`. Each resolves once and panics naming the field: a call timeout is 1s when left out, at least 1ms, unlimited refused, where a non-positive one took 1s and a sub-millisecond one was kept; `ScanCount` is 256, at least 1, where a non-positive one took 256; `MaximumClockSkew` is 0, no tolerance, at least 0, a negative one refused naming the field; `RevocationEpochRetention` is 7 days, at least 1s, where a sub-second one was kept, and `bound.UnlimitedDuration()` keeps a boundary for ever, as the in-memory store does, a boundary an earlier retention gave an expiry made persistent by the next revocation. [`ModuleConfig`](../../integrations/rueidis/v4/module.go)`.Locker` and `.TokenStore` replace `LockerOptions` and `TokenStoreOptions`, both resolved at `RegisterServices`, so a value outside its rule fails the registration at boot, where it failed the container's first resolve; the container's clock reaches a token store whose config names none and leaves one that names its own. `NewLockerWithOptions`, `LockerOption`, `WithLockerCallTimeout`, `NewNonceGuardWithPrefix`, `NewNonceGuardWithOptions`, `NonceGuardOption`, its two options, `TokenStoreOption`, its six options and `RegisterLockerServiceWithOptions` are removed.

**Symptom.** `NewLocker(client)`, `NewNonceGuardWithPrefix(client, prefix)`, `NewTokenStore(client, WithTokenStorePrefix(prefix))`, `RegisterLockerService(registrar, client)` and `ModuleConfig{TokenStoreOptions: ...}` no longer compile. A call timeout set to `bound.Of(0)` or under a millisecond, a scan count of `bound.Of(0)`, a negative skew and a retention under a second panic at construction, or at the registration; a field left at its zero value takes the default.

**Remedy.** `rueidis.NewLocker(client, rueidis.LockerConfig{})`, `CallTimeout: bound.Of(500 * time.Millisecond)` for `WithLockerCallTimeout`; `rueidis.NewNonceGuard(client, rueidis.NonceGuardConfig{KeyPrefix: prefix})`; `rueidis.NewTokenStore(client, rueidis.TokenStoreConfig{Prefix: prefix, Clock: clock})`, each option a field, a duration through `bound.Of`; `ModuleConfig{Locker: &rueidis.LockerConfig{}}`; `ModuleConfig{TokenStore: &rueidis.TokenStoreConfig{Prefix: prefix}}`.

### Rueidis: the rate limiter and the server-sent event backplane are built from one struct each, and the reconnect backoff is refused where it was clamped

**What changed.** [`NewRateLimiter(client, name, limit, window, config)`](../../integrations/rueidis/v4/rate_limit.go) takes the client, the name, the limit and the window positionally, as the in-process limiters do, a malformed name, a limit below 1 and a window below 1ms refused naming the argument, and a `RateLimiterConfig` of `KeyPrefix`, `FailureMode` and `CallTimeout`: `FailureMode` is `FailureModeClosed` when left out and any other value than the two constants is refused, and `CallTimeout` is 250ms, at least 1ms, unlimited refused. [`NewServerSentEventBackplane`](../../integrations/rueidis/v4/server_sent_event_backplane.go) takes the client, the hub and a `ServerSentEventBackplaneConfig` of `Channel`, `Logger`, `Reconnect` and `CallTimeout`, the amqp backplane's name, `CallTimeout` 1s, at least 1ms, unlimited refused; the configuration is resolved before the backplane is installed on the hub, so a refusal leaves the hub without one. [`ReconnectConfig`](../../integrations/rueidis/v4/reconnect_config.go) is a value of one `Backoff bound.Backoff`, `Initial` and `Max` bound limits of 1s and 30s, at least 1ms, unlimited refused, and `Factor`, 2 when left out; an initial backoff above the cap is refused naming both fields, and a factor below 1 or NaN is refused. A value outside its rule panics at construction, naming the field.

**Symptom.** `NewRateLimiter(client, 60, time.Minute, WithRateLimiterKeyPrefix(prefix))`, `NewServerSentEventBackplane(client, hub, WithServerSentEventBackplaneLogger(logger))`, `DefaultReconnectConfig()` and `NewReconnectConfig(...)` no longer compile. A window under a millisecond, which was raised to 1ms, a misspelt failure mode, which was read as closed, a call timeout of `bound.Of(0)`, which took the default, a positive call timeout under a millisecond, which was kept, an initial backoff above the cap, which was clamped onto it, and a backoff factor below 1, which fell back to 2, now panic at construction; a field left at its zero value takes the default. An empty `KeyPrefix` is the default `melody:rate_limit:`, where `WithRateLimiterKeyPrefix("")` kept the counters under no prefix at all; a deployment that ran with the empty prefix starts every counter afresh under the default one, and its instances must move together, or they count on two namespaces.

**Remedy.** `rueidis.NewRateLimiter(client, "api", 60, time.Minute, rueidis.RateLimiterConfig{KeyPrefix: prefix})`, each option a field, `CallTimeout: bound.Of(500 * time.Millisecond)` for `WithRateLimiterCallTimeout`; `rueidis.NewServerSentEventBackplane(client, hub, rueidis.ServerSentEventBackplaneConfig{Channel: "app:sse", Logger: logger})`; `Reconnect: rueidis.ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(time.Minute)}}` for a reconnect configuration of your own, an initial backoff no higher than the cap.

### Rueidis cache: the backend is built from one struct, and every round trip is bounded at 1s by default

**What changed.** [`cache.NewBackend`](../../integrations/rueidis/v4/cache/backend.go) takes the client and a `BackendConfig` of `Prefix`, `ScanCount`, `DeleteBatch`, `MaxKeyLength` and `CallTimeout`; [`NewBackendService`](../../integrations/rueidis/v4/cache/backend_service.go) takes the client and a `BackendConfig`, and [`RegisterBackendService`](../../integrations/rueidis/v4/cache/service_resolver.go) takes one too, resolved at the call; [`ModuleConfig.Backend`](../../integrations/rueidis/v4/cache/module.go) replaces `Prefix` and `BackendOptions`. `ScanCount` 1000, `DeleteBatch` 500 and `MaxKeyLength` 1024 when left out, at least 1, unlimited refused; `CallTimeout` bounds every round trip on top of the caller's context, 1s when left out, at least 1ms, unlimited refused. A value outside its rule is the error of `NewBackend` and `NewBackendService`, naming the field, and panics at `RegisterBackendService`.

**Symptom.** `NewBackend(client, ctx, prefix, 0, 0, WithCommandTimeout(time.Second))`, `RegisterBackendServiceWithOptions(...)` and `ModuleConfig{Prefix: ..., BackendOptions: ...}` no longer compile. A backend built or registered without a call timeout, whose reads ran unbounded, now refuses one that takes longer than a second with the context's deadline; a scan count, delete batch or key length of `bound.Of(0)`, which took the default, is refused; a nil client fails `RegisterBackendService` at the call, where it failed the first resolve; a field left at its zero value takes the default.

**Remedy.** `cache.NewBackend(client, cache.BackendConfig{Prefix: prefix})`; `CallTimeout: bound.Of(time.Second)` for `WithCommandTimeout` and `MaxKeyLength: bound.Of(n)` for `WithMaxKeyLength`; `cache.RegisterBackendService(registrar, client, cache.BackendConfig{Prefix: "app:"})`; `cache.ModuleConfig{Client: client, Backend: cache.BackendConfig{Prefix: "app:"}}`. A composition whose operations legitimately run longer than a second raises `CallTimeout`. A `Clear` or a `ClearByPrefix` spends it on each `SCAN` and `DEL` round trip, so a large namespace needs neither. A round trip the call timeout cut answers an error naming `BackendConfig.CallTimeout`, with `cache.ErrCallTimeout` in its chain: match it with `errors.Is(err, context.DeadlineExceeded)` or `errors.Is(err, cache.ErrCallTimeout)`, never `err == context.DeadlineExceeded`.

### Amqp: the provider is built from one struct, the reconnect backoff is bound limits inherited field by field and refused where it was clamped, the transport's limits are bound limits, and the heartbeat and the close bound are configured

**What changed.** [`amqp.NewProvider`](../../integrations/amqp/v4/connection.go) takes one `ProviderConfig` of `Heartbeat`, `Reconnect` and `CloseTimeout`: the heartbeat is always installed, 10s when left out, at least 1s, unlimited refused; `CloseTimeout` bounds each close stretch that cannot observe its signal, 30s when left out, at least 1ms, unlimited refused. [`ReconnectConfig`](../../integrations/amqp/v4/reconnect_config.go) is a value of the bound limits `Backoff.Initial` 1s and `Backoff.Max` 30s, at least 1ms, unlimited refused, and `Backoff.Factor`, 2 when left out; `TransportConfig` and `ServerSentEventBackplaneConfig` take it by value, and each field they leave out is the provider's for a component the provider built and the default otherwise, as `CloseTimeout`, which both gained, is. An initial backoff above the cap is refused naming both fields, judged on the merged backoff, and a factor below 1 or NaN is refused. [`TransportConfig`](../../integrations/amqp/v4/transport.go)'s `Prefetch` and `PublishReturnBuffer` are `bound.Count` and `CallTimeout` a `bound.Duration`: `Prefetch` 10 when left out, the figure `melody.amqp.prefetch` registers, at least 1 and at most 65535; `PublishReturnBuffer` 16 and `CallTimeout` 30s, at least 1 and 1ms. The backplane's `CallTimeout` is a `bound.Duration`, 1s, at least 1ms, resolved before the backplane is installed on the hub, and a typed-nil `Logger` records nothing. Every value outside its rule panics at construction naming the field. `ProviderOption`, `WithHeartbeat`, `WithReconnectConfig`, `DefaultReconnectConfig` and `NewReconnectConfig` are removed.

**Symptom.** `NewProvider()`, `NewProvider(WithHeartbeat(d))`, `NewProvider(WithReconnectConfig(NewReconnectConfig(initial, max, factor)))`, `Reconnect: &ReconnectConfig{...}`, `Prefetch: 10`, `PublishTimeout: d` and `CallTimeout: d` no longer compile. A transport built without `Prefetch` prefetches 10 where it prefetched 1. A boot panics where it used to clamp a `Prefetch` above 65535, pass a heartbeat under a second as the server's interval, start a backoff above its cap, read a factor below 1 as the layer under it, or read a zero `PublishTimeout` or `CallTimeout` as the default.

**Remedy.** `amqp.NewProvider(amqp.ProviderConfig{})`; `Heartbeat: bound.Of(d)` for `WithHeartbeat`, `Reconnect: amqp.ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(initial), Max: bound.Of(max), Factor: factor}}` for the reconnect option and the transport's pointer, `bound.Of(n)` and `bound.Of(d)` for the transport and backplane limits; leave a field out for its default, or for the provider's. `Prefetch: bound.Of(1)` keeps the one-at-a-time consumer.

### Outbox: the relay's limits are bound limits refused where they were clamped or raised, its detached budgets are configured, and the relay command takes a configuration struct

**What changed.** [`RelayConfig`](../../integrations/outbox/v4/relay.go)'s `LockTtl`, `VisibilityTimeout`, `Backoff.Initial` and `Backoff.Max` are `bound.Duration` and `BatchSize`, `MaxAttempts` and `MaxDeliveryAttempts` `bound.Count`, resolved once at `NewRelay` with the defaults they had, a value outside its rule panicking naming the field: `LockTtl` and `VisibilityTimeout` at least 1s, the backoffs at least 1ms, the counts at least 1, unlimited refused. `BatchSize` above 100000 is refused where it was clamped, and so is a `Store.ClaimDueMessages` limit above it; `MaxDeliveryAttempts` at or below `MaxAttempts` is refused naming both where it was raised to twice `MaxAttempts`, which stays its default; a `Backoff.Initial` above `Backoff.Max` is refused naming both, and a `Backoff.Factor` below 1 or NaN is refused. `ResolutionTimeout`, 5s when left out as the constant was, bounds one resolution write. [`NewRelayCommand`](../../integrations/outbox/v4/relay_command.go) takes a `RelayCommandConfig` of `ErrorBackoff`, capped at one minute when left out, and `ModuleConfig.RelayCommand` carries it to the module's command.

**Symptom.** `BatchSize: 50`, `LockTtl: 30 * time.Second`, `MaxAttempts: 3` and the other limits no longer compile, nor does `NewRelayCommand(relay)`. A boot panics where a zero took the default, a `BatchSize` above 100000 was clamped or a `MaxDeliveryAttempts` at or below `MaxAttempts` was raised, a `LockTtl` or `VisibilityTimeout` under a second was taken, or an initial backoff above the cap was held at the cap.

**Remedy.** `bound.Of(n)` and `bound.Of(d)` for each limit, or leave it out for its default; a `MaxDeliveryAttempts` above `MaxAttempts`, or none; `outbox.NewRelayCommand(outbox.RelayCommandConfig{})`, with `ErrorBackoff: bound.Backoff{Max: bound.Of(d)}` to change the cap.

### Cron: the in-process runner is built from one struct, an entry's timeout is a bound limit of one hour by default with unlimited written out, and the output capture and the clock jump threshold are configured

**What changed.** [`cron.NewRunnerCommand`](../../integrations/cron/v4/runner_command.go) takes the configuration and one [`RunnerConfig`](../../integrations/cron/v4/runner_config.go) of `Dialect`, `Commands`, `OutputCaptureLimit`, `ClockJumpResetThreshold`, `Timeout` and `ShutdownTimeout`, resolved once, a value outside its rule panicking naming the field; [`ModuleConfig.Runner`](../../integrations/cron/v4/module.go) replaces `RunnerCommands` and `RunnerDialect`. `OutputCaptureLimit` is 64 KiB when left out, at least 1; `ClockJumpResetThreshold` 3h, at least 2m, in whole minutes; `Timeout` 1h, at least 1ms, unlimited admitted; `ShutdownTimeout` 5m, at least 1ms; every other unlimited is refused. [`Configuration.Schedule`](../../integrations/cron/v4/configuration.go) takes the `EntryConfig` by value and `ScheduledCommand.Config` is a value. `EntryConfig.Timeout` and `ShutdownTimeout` are `bound.Duration`, the runner's when left out, at least 1ms, an unlimited timeout admitted and an unlimited window refused; `EntryConfig.Instances` is a `bound.Count`, 1 when left out, at least 1, refused naming the field by the generator, as its error, and by `NewRunnerCommand`. An entry refusal names the field and the entry's command. An entry that names no timeout has its context cancelled after one hour where it ran with no deadline, and a command that ignores the cancellation is abandoned one `ShutdownTimeout` later, its container scope closed, so the runner stops holding the run; the cancellation is not a kill, and a command wedged on a read that ignores its context keeps its own goroutine until that read returns, so it uses cancellable I/O.

**Symptom.** `NewRunnerCommand(configuration, cron.RunnerDialectCrontab, commandA, commandB)`, `ModuleConfig{RunnerCommands: ...}`, `Schedule(name, &cron.EntryConfig{...})` and `Timeout: 5 * time.Minute` no longer compile. A job that legitimately runs longer than an hour under the runner, with no timeout of its own, is cancelled at the hour and reported as timed out. A boot panics where a negative timeout meant no deadline, a non-positive window took the default, or a negative `Instances` generated one instance.

**Remedy.** `cron.NewRunnerCommand(configuration, cron.RunnerConfig{Commands: []clicontract.Command{commandA, commandB}})`; `ModuleConfig{Runner: cron.RunnerConfig{Commands: commands}}`; `Schedule(name, cron.EntryConfig{...})`; `Timeout: bound.Of(5 * time.Minute)`. An entry that should run with no deadline writes `Timeout: bound.UnlimitedDuration()` where it wrote a negative value; a runner that wants none for every entry naming no timeout writes it on `RunnerConfig.Timeout`.

### Websocket: the idle timeout is 30s when left out, the read limit and the write timeout are bound limits with unlimited written out, and the close handshake grace is configured

**What changed.** [`websocket.StreamHandlerConfig`](../../integrations/websocket/v4/handler.go)' `WriteTimeout` and `IdleTimeout` are `bound.Duration` and `ReadLimit` a `bound.Count`, resolved once at `NewStreamHandler`, a value outside its rule panicking naming the field, at boot through the module. `IdleTimeout` is 30s when left out, at least 1s, unlimited refused; `WriteTimeout` 10s, at least 1ms, unlimited refused; `ReadLimit` 32 KiB, written on every connection, at least 1, unlimited admitted to disable the limit. The new `CloseHandshakeGrace` bounds the closing handshake on the teardown path, 1s when left out, at least 1ms, unlimited refused.

**Symptom.** `IdleTimeout: 30 * time.Second`, `WriteTimeout: d` and `ReadLimit: n` no longer compile. `Options{}` builds a handler pinging every 30s where it panicked. A boot panics where a negative `ReadLimit` disabled the limit, a zero `WriteTimeout` took 10s, or an `IdleTimeout` below a second was taken.

**Remedy.** `IdleTimeout: bound.Of(30 * time.Second)`, or leave it out for the same interval; `ReadLimit: bound.UnlimitedCount()` where you wrote a negative value, `bound.Of(n)` for a cap; `WriteTimeout: bound.Of(d)`.

### Opentelemetry: the tracer provider takes the runtime

**What changed.** [`otlp.NewTracerProvider(runtimeInstance, config)`](../../integrations/opentelemetry/v4/otlp/tracer_provider.go) takes the runtime in place of a context: it dials its exporter under the runtime's context, refuses a nil runtime and refuses a runtime whose context is already done before the dial. The module's provider hands it `resolver.Runtime()`, and `RegisterHttpMiddlewares` resolves it through `Kernel.BootRuntime()`.

**Symptom.** `cannot use ctx (context.Context) as containercontract.Runtime` in a call of `otlp.NewTracerProvider`.

**Remedy.** Inside a container provider pass `resolver.Runtime()`; in a boot hook resolve the provider through `kernelInstance.BootRuntime()`.

### Opentelemetry: the sampling is a typed value with none written out, the batch timeout and the shutdown bound are bound limits, and the prometheus composing twin is removed

**What changed.** [`otlp.Config.SampleRatio`](../../integrations/opentelemetry/v4/otlp/tracer_provider.go) is replaced by `Config.Sampling`, an [`otlp.Sampling`](../../integrations/opentelemetry/v4/otlp/sampling.go) whose zero value samples every trace, `SampleRatio(x)` a fraction in `(0, 1]` and `SampleNone()` none; a ratio of 0, one above 1 and NaN are refused at `NewTracerProvider` naming `Config.Sampling`. `Config.BatchTimeout` is a `bound.Duration`, 5s when left out, at least 1ms, unlimited refused. `NewMetricsMiddlewareWithPrometheus` is removed.

**Symptom.** `SampleRatio: 0.25`, `BatchTimeout: d` and `opentelemetry.NewMetricsMiddlewareWithPrometheus(name)` no longer compile. `SampleRatio(0)`, which sampled every trace as the zero ratio, and a ratio above 1, read as 1, are refused.

**Remedy.** `Sampling: otlp.SampleRatio(0.25)`, leave `Sampling` out to sample everything, `otlp.SampleNone()` to sample no trace that starts here — the sampler is parent-based, so a request carrying a sampled `traceparent` is still traced and exported, and an edge that must trace nothing strips the inbound header; `BatchTimeout: bound.Of(d)`. `NewPrometheusMetrics(PrometheusConfig{MeterName: name})`, then `NewMetricsMiddleware(prometheusMetrics.Meter(), MetricsMiddlewareConfig{})` and `prometheusMetrics.Handler()`, or `ModuleConfig.Prometheus`, for the removed twin.

### Aws s3: the client dials TLS unless Insecure is set, the bucket and storage doors take a configuration struct, and the put spool and the multipart abort are bound limits

**What changed.** [`awss3.Config.Secure`](../../integrations/aws/s3/v4/provider.go) is replaced by `Config.Insecure`, so the zero value dials https; the refusals of an empty endpoint or credential name the field. [`EnsureBucket`](../../integrations/aws/s3/v4/provider.go) takes an `EnsureBucketConfig` of `Region` after the bucket, the client's region when empty. [`NewStorage`](../../integrations/aws/s3/v4/storage.go) takes a `StorageConfig` after the bucket, [`RegisterStorageService`](../../integrations/aws/s3/v4/service_resolver.go) one too, resolved at the call, and [`ModuleConfig.Storage`](../../integrations/aws/s3/v4/module.go) carries it: `PutSpoolLimit`, what a `Put` may hold in memory, 16 MiB when left out, at least 1, and `MultipartAbortTimeout`, the bound of the detached abort, 10s when left out, at least 1ms, both refusing unlimited, naming the field.

**Symptom.** `Secure: true`, `EnsureBucket(ctx, client, bucket, region)`, and `NewStorage(client, bucket)` no longer compile. A configuration that named neither `Secure` nor `Insecure` dials https where it dialed plaintext http, so a plaintext endpoint answers the TLS handshake with an error.

**Remedy.** Drop `Secure: true`; write `Insecure: true` where `Secure` was false on purpose, for a local endpoint. `EnsureBucket(resolver.Runtime(), client, bucket, awss3.EnsureBucketConfig{Region: region})` from a provider registered `RegisterOptions{BuiltAtBoot: true}`, resolved by what writes to the bucket, so a missing bucket is a boot warning and the first use checks it again (`BootFailure: container.BootFailureFailsBoot` keeps it failing `Boot`), or hand the module `ModuleConfig.BucketCheck{Enabled: true, Region: region}`, which has the storage's own provider check it; `EnsureBucket` refuses a nil runtime and bounds its calls by the runtime's context; `NewStorage(client, bucket, awss3.StorageConfig{})`; the module registers the storage from `ModuleConfig{Client, Bucket, Storage}`.

### Event: `NewEventDispatcherAdapter` takes the dispatcher alone

**What changed.** `event.NewEventDispatcherAdapter` drops its clock parameter, which the adapter never read; the third major kept it in the released signature and refused a nil one.

**Symptom.** A call that passes a clock no longer compiles.

**Remedy.** Delete the clock argument: `event.NewEventDispatcherAdapter(dispatcher)`.

### Cron: the three deprecated short-name aliases are removed

**What changed.** `integrations/cron/v4` drops `ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars`, which forwarded to `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters`.

**Symptom.** Code that names an alias no longer compiles.

**Remedy.** Use the spelled-out names; they behave as the aliases did.

### Wiring: the generated source is written in gofmt's form

**What changed.** `melody:wiring:generate` and `wiring.Generate` pass the rendered source through `go/format`, so the generated file is indented with tabs and is a fixed point of gofmt; it was indented with four spaces. The file written, the source printed to stdout and the source a staleness check regenerates in memory are the same bytes, and a source that does not format is refused with the rendered text in the error's context and no file written. Shipped in `v3.14.1`.

**Symptom.** A staleness check that compares the committed file with `wiring.Generate` reports the file stale until it is regenerated, and the regeneration rewrites the whole file.

**Remedy.** Regenerate the file once with `melody:wiring:generate`, and drop any exclusion of the generated file from `gofmt -l`.

### Security: an HMAC envelope's issue time is read with its one-second resolution

**What changed.** `HmacTokenSource` tolerates one second on top of `Leeway` when it checks that an envelope is not issued in its future. `IssuedAt` carries whole seconds, the signer's instant truncated down, so a receiver whose clock trailed the sender's by a millisecond across a second boundary refused the envelope as not yet valid. The expiry and the nonce ttl are not widened. Shipped in `v3.14.2`.

**Symptom.** A service-to-service call that was answered anonymous now and then (about one envelope in five hundred at a 2ms skew with a zero `Leeway`, the third major's default) authenticates; an envelope stamped up to a second ahead of the receiver's clock is accepted where it was refused.

**Remedy.** None. A deployment that set `Leeway` only to absorb this effect can lower it back.

### Security: a JSON web token's `nbf` and `iat` are read with their one-second resolution

**What changed.** `JwtTokenValidator` reads `nbf` and `iat` rounded down and tolerates one second on top of `Leeway` when it checks that a token is not yet valid or issued in its future. Both were read rounded up and compared with the clock as they were, so a validator whose clock trailed the issuer's by a millisecond across a second boundary refused a fresh token, and a fractional claim was refused during its whole first second. The expiry is not widened, and the revocation boundary compares `iat` rounded down as before. Shipped in `v3.14.2`.

**Symptom.** A fresh token that was refused now and then as "jwt is not yet valid" or "jwt is issued in the future" (the latter by default, `AllowFutureIssuedAt` being the door) is accepted; a token whose `nbf` or `iat`, read rounded down to the second, is up to a second ahead of the validator's clock is accepted where it was refused, so a fractional claim may be up to just under two seconds ahead.

**Remedy.** None. A deployment that set `Leeway` only to absorb this effect can lower it back.

### Application: a command journals to standard error when no log path is configured

**What changed.** With an empty `MELODY_LOG_PATH` the container logger of a command writes to standard error; the http process still writes to standard output. A command's standard output carries what it prints, so a document redirected to a file no longer holds the boot records. Shipped in `v3.14.2`.

**Symptom.** A process supervisor or a log shipper that read a worker's or a command's journal from standard output finds it on standard error.

**Remedy.** Collect standard error too, or set `MELODY_LOG_PATH` to a file.

### Mailer: a custom header key is emitted trimmed, and one that is not a field name is refused

**What changed.** `RenderMessage`, and so every transport, emits a key of `Message.Headers` trimmed, as the reserved and Message-ID checks already judged it, and refuses a key that is empty or carries a byte outside the printable characters other than the colon. Shipped in `v3.14.2`.

**Symptom.** A message whose custom header key was padded goes out with the trimmed name; a message whose key was empty or carried a space, a colon or a non-ASCII byte fails to render with "mailer header name …" where it went out with a malformed header.

**Remedy.** Write each custom header key as a field name, `X-Campaign`.

### Http: the compression middleware types an untyped body before compressing it

**What changed.** A response that carries a body and no `Content-Type` is given the type net/http would sniff from its first bytes before the compression middleware compresses it, and a sniffed type the configuration excludes goes out uncompressed. net/http sniffs an identity body but never an encoded one, so the gzip representation went out with no type. Shipped in `v3.14.2`.

**Symptom.** A client that negotiates gzip receives the `Content-Type` a client that does not receives; a `FileResponse` of an extensionless image is no longer compressed.

**Remedy.** None. Set `Content-Type` on the response to choose the type rather than have it sniffed.

### Storage: locally a key names an object only by a regular file

**What changed.** [`LocalStorage`](../storage/local.go) treats a key that resolves to a directory, a named pipe or a device as no object, and a key that runs through a regular file (`object.bin/child`) as absent. Shipped in `v3.14.2`.

**Symptom.** `Delete` of a key naming an empty directory leaves the directory in place, where it removed it; in this major it also refuses the key (the section "Storage: deleting a key that names a directory is an error"). `Exists` and `Delete` of a key under a regular file answer `false` and `nil`, where they answered a "not a directory" error. `Get` of a named pipe answers an error at once, where it blocked, and `Exists` answers `false` for it.

**Remedy.** None for an application that stores objects through `Put`. One that removed directories through `Delete` removes them itself.

### Mailer: an address's Email is a bare addr-spec

**What changed.** `Manager.Send` refuses an `Address.Email` that is not a bare addr-spec (`bob@example.com`): a display name, angle brackets, a comment or surrounding spaces, which the address parser accepted and the renderer and the SMTP envelope then wrote verbatim, as `RCPT TO:<Bob <bob@x.com>>`. Shipped in `v3.14.2`.

**Symptom.** `Send` answers `mailer message address is not a bare addr-spec; put a display name in Name` for such an address, and nothing is sent.

**Remedy.** Put the display name in `Address.Name` and the bare address in `Address.Email`; trim user input before it reaches the mailer.

### Config: a separatorless last line of a .env file is refused

**What changed.** A `.env` line without `=` or `:` is refused on the last line of a file, with or without a final newline, as it already was on every other line; it used to be dropped without a word. Shipped in `v3.14.2`.

**Symptom.** A boot that read such a file now fails naming the file, where it booted without the line — a `MELODY_ENV` written last without its value booted the default environment.

**Remedy.** Give the line its value, or remove it.

### Storage: a key that carries a `..` segment is refused

**What changed.** `LocalStorage` and the awss3 `Storage` refuse, at every door, a key that holds a `..` segment once its backslashes are read as separators, with an error that names the segment. They used to fold it: `tenant-b/../tenant-a/secret.txt` addressed `tenant-a/secret.txt` on both backends.

**Symptom.** `Put`, `Get`, `Stat`, `Delete`, `storage.Exists` (and `PresignedUrl` on awss3) answer `storage key carries a ".." segment` or `object storage key carries a ".." segment` for a key they used to fold.

**Remedy.** Address the object by the key it folds to: `b` for `a/../b`. A key built from a client-chosen name should refuse a separator in that name before it reaches the storage.

### HTTP: the rate limiters key an IPv6 client on its /64

**What changed.** The default key of the rate limiters (`RateLimitMiddleware`, `RegisterRateLimitRequestListener`, `SimpleRateLimit`, `IpRateLimit` and the address fallback of `UserRateLimit`, with or without a `ClientIpResolver`) aggregates an IPv6 client address to its /64. An IPv4 address, written bare or mapped into IPv6 (`::ffff:203.0.113.7`), keys on itself as before. One host is routinely handed a /64, so keyed on the /128 it could rotate its source address and receive a fresh budget for every request: 100 requests from 100 addresses of one /64 all passed a budget of 5. With a key ceiling set, the same host also filled the key table, after which every new client was refused.

**Symptom.** IPv6 clients that share a /64 now share one budget, so a deployment whose clients sit behind one IPv6 network may see them refused sooner. `DefaultClientIp` and a `ClientIpResolver` still answer the address itself; only the key the limiter counts under changes.

**Remedy.** Size the budget for the clients of one /64, or key the limiter on something else through `RateLimitConfig.KeyExtractor` (an authenticated identity through `UserRateLimit`). There is no opt-out to the /128: it is the defect the change closes.

### Security: an access control rule path begins with a slash and is canonical, and a raw prefix declares its reach

**What changed.** Every access control rule path that does not begin with a slash, or that carries an empty, `.` or `..` segment, is refused at declaration, by the `accesscontrol` constructors, the builder's `Require` and `AllowAnonymous` and the deprecated `security` constructors; a raw prefix rule that ends with a slash is refused as well, and `accesscontrol.NewControl` refuses the zero `Rule`. The empty raw prefix, the declared fallback, and `/` are unchanged.

**Symptom.** The application panics at boot with `access control rule path must begin with a slash`, `access control rule path must be canonical`, `access control raw prefix rule may not end with a slash` or `access control rule carries no attribute`. The rule it names never did what it said: a path without a slash or spelled `//admin` or `/x/../admin` matched nothing, since the request path is matched cleaned, so its paths reached their handlers without a decision; a raw `/api/` claimed `/api-internal` and outranked a regex rule written for that tree; a zero `Rule` denied every path no other rule claimed.

**Remedy.** Write the path with its leading slash, cleaned: `/admin` for `//admin` or `/x/../admin`. For a raw rule written with a trailing slash, declare the reach it meant: `accesscontrol.NewSegmentPrefixRule("/api", …)` for the segment, or the raw prefix without the slash when `/api-internal` belongs to it. Build every `Rule` with a constructor.

### Security: a firewall's name is declared once, and its rules are never nil

**What changed.** The builder and `Compile` refuse a second firewall of a name already declared, and a nil or typed-nil rule in a firewall's rule list; `NewCompiledFirewall` refuses the nil rule too.

**Symptom.** The application panics at boot with `security firewall name is already declared` or `security firewall rule is nil`. Two firewalls of one name were answered as the first by the registry and as the last by the manager; a nil rule answered every request behind its firewall with a 500.

**Remedy.** Give each firewall its own name, and remove the nil entry from its rule list.

### Security: rejected credentials dispatch `security.login.failure`

**What changed.** When the authenticator that supported the request answers no user, `AuthenticatorTokenSource` dispatches `security.login.failure`, carrying `security credentials rejected`, before the request goes on anonymous. A wrong `X-Api-Key` is the common case. A second factor supplied and refused, a wrong or replayed TOTP code or a recovery code the store did not redeem, dispatches it too, carrying `security second factor rejected`; a pending second factor the request has not answered yet dispatches nothing.

**Symptom.** A listener on `security.login.failure` is called for a guessed or stale api key, where it was called only when an authenticator answered an error; a failing listener now fails that request, as it already failed a successful one.

**Remedy.** None for a listener that audits or counts failures. One that assumed the event always carries an authenticator's error reads `LoginFailureEvent.Error()` as a description only.

### Application: the shutdown drain runs whatever failed before it

**What changed.** The http wind-down no longer returns on its first failing phase: the serve result, the server shutdown and the request-scope drain each run, and each failing one contributes its cause.

**Symptom.** A shutdown whose serve or server-shutdown phase failed starts reporting the open request scopes as well, joined with the earlier cause; a run that failed for one reason alone reads exactly as it did.

**Remedy.** None. Code matching on the run error by string should read it with `errors.Is`, which traverses the join.

### Http: the session-cookie cache guard keeps the rest of `Cache-Control`

**What changed.** The guard that keeps a session-cookie response out of a shared cache reads every `Cache-Control` field line rather than the first, and splits the directive list outside quoted sections rather than on a bare comma.

**Symptom.** A response that carried `Cache-Control` on more than one field line keeps the directives on the lines behind the first, which used to be deleted; a directive carrying a quoted field-name list keeps every name in it, where a name spelled like a directive used to be dropped out of the middle of the list.

**Remedy.** None.

A `Cache-Control` line that ends inside a quoted string is the one exception: no directive of it can be read safely, and an appended `private` would land inside the quote, so the guard replaces the whole header with `Cache-Control: no-store, private`, keeping nothing of what the application wrote. **Remedy** for that case: close the quote; a response whose `Cache-Control` the guard cannot read is not cached at all.

### Container: a collection refuses a closed scope, and a live-scope registration's options are honoured or refused

**What changed.** `AllImplementing` gathered through the resolver a provider was handed refuses a closed scope, the way collecting through the scope itself already did — the refusal was keyed on a method the resolution context did not implement. `RegisterScoped` on a live scope honours `RegisterOptions.CollectionPriority` instead of accepting and dropping it, and refuses `TeardownDependencyNames` with `ErrScopedTeardownDependencyUnsupported`, the way the container's scoped door already refuses it.

**Symptom.** A request-outliving goroutine collecting through a captured resolver receives `ErrScopeClosed` where it used to receive an empty set and dispatch to nobody. A scoped registration that declared a collection priority starts being ordered by it. A scoped registration that declared a teardown dependency on a live scope now fails at the registration call instead of installing nothing.

**Remedy.** Handle `ErrScopeClosed` from a collection made after the request ended — it is the same refusal `Get` already answers there. Drop the `TeardownDependencyNames` from a live-scope registration: a scope keeps its own teardown graph, recorded from the resolutions it actually made, so there was never anywhere for the declaration to be written.

### Http: a catch-all keeps an encoded separator inside its segment

**What changed.** The request path is split on the separators the client actually sent and each segment is unescaped on its own, so a parameter may carry a slash; the catch-all rebuilt its value by joining the decoded segments back on `/`, which put the separator back where a segment boundary is. Only the separator is now re-escaped when the tail is rebuilt.

**Symptom.** A catch-all value may now contain `%2F` where the request spelled the separator that way — `/files/a%2Fb/c` binds `a%2Fb/c` and is finally distinguishable from `/files/a/b/c`, which still binds `a/b/c` unchanged. A requirement regex applied to a catch-all sees that spelling.

**Remedy.** A handler that reassembles a storage key or a proxy target from the catch-all replaces the re-escaped separator (`strings.ReplaceAll(value, "%2F", "/")`) if it wants the literal slash — not `url.PathUnescape`, which would decode a second time every other escape the segments already decoded; one whose paths never carry an encoded separator sees no change. A requirement that must admit the encoded spelling names it.

The access-control matcher, the firewall path matcher, the kernel's canonical-path guard, the static file server and its exclusion list, the compression exclusion list and the internal-auth endpoint binding read that same spelling (`http.RequestPathAsRouted`), not the decoded `URL.Path`: `/public%2F` is the resource `public/`, distinct from `/public`, so an exact rule written for `/public` does not claim it — measured, read decoded it was folded onto the public rule of `/public` while the router served it through a protected catch-all handler, to an anonymous client — and a firewall or rule written for `/admin/` does not claim `/admin%2Fusers`, a one-segment resource the router never routes under `/admin`. A rule that means to claim a resource whose name carries a separator names it with `%2F`, and so does an internal-auth caller signing the path of such a request (`HmacEnvelopeSigner.Sign` is handed the path in the routed spelling — each segment decoded, a separator encoded inside a
segment kept as `%2F`: `/files/a%2Fb` for that resource, `/files/café` for one whose segment carries no separator, never the escaped request line). The static file server resolves a file from the routed spelling, so `/static/private%2Fsecret.txt` names a file whose name carries `%2F` and no longer reaches `private/secret.txt` beneath the mount.

The internal-auth binding goes one step further and refuses a request whose path carries a segment that decodes to the literal text `%2F` (sent as `%252F`): its routed spelling is the one a separator encoded inside a segment has, so an envelope signed for `/files/a%2Fb` authenticated `/files/a%252Fb`, which the router binds to another parameter value. Signing again does not help, since no signed path can tell the two apart: an internal-auth caller that addresses a resource whose name carries the text `%2F` renames it, or carries the name in the query or the body instead of the path. The signed spelling of every other path is unchanged.

### Validation: the string-form constraints operate on strings

**What changed.** `regex`, `email`, `alpha`, `alphanumeric` and `numeric` refuse a value that is not a string instead of silently passing it, and `min`, `max` and `notBlank` no longer measure the fmt rendering of a non-string — all three refuse the type. A nil pointer and the empty string still pass the five format constraints, so optional-field composition is unchanged.

**Symptom.** A string-form rule on a non-string field — `regex` on `[]byte`, `max=10` on an `int`, `notBlank` on a `bool` — now rejects every value with `value must be a string`. It previously either passed everything (the format five, and `notBlank` on `false`/`0`/empty collections) or measured the Go rendering: an empty slice passed `min=1` as the two runes of `[]`, and a five-byte `[]byte` failed `max=5` at a rendered width of twenty-one.

**Remedy.** Move the rule to a string field, or use the constraint built for the type: [`greaterThan`/`lessThan`](../validation/constraint_greater_than.go) for numeric ranges, [`notEmpty`](../validation/constraint_not_empty.go) for collections. A rule that must inspect a non-string type is a custom [`contract.Constraint`](../validation/contract/constraint.go).

### Validation: a parameterized rule needs its parameters, whole and non-negative

**What changed.** Three related refusals in how rule parameters are read. A parameterized rule named without parameters (`regex`, `regex()`, `max()`, `lessThan`) fails closed instead of validating with the registered singleton — the bare `regex` validated with the match-everything `.*` default, the bare `lessThan` meant "less than 0", the bare `max` meant the constructor's 100. A numeric bound must be an integer in its entirety: the parse previously accepted a valid leading integer and dropped the rest, so `lessThan=-0.5` became a bound of 0 and accepted `-0.2`, which the tag as written refuses. And [`Regex.WithParameters`](../validation/constraint_regex.go) refuses an empty pattern, while [`MinLength`](../validation/constraint_min_length.go) and [`MaxLength`](../validation/constraint_max_length.go) refuse a negative bound.

**Symptom.** A tag of any of these shapes now reports `invalidRuleSyntax` on every value it reaches, where it previously enforced a configuration nobody wrote — or nothing at all. The refusal reason travels in the error context under `cause`, and because `invalidRuleSyntax` is one of the rule-wiring codes the kernel already classifies, the 400 is recorded at error level and that context stays out of the response body.

**Remedy.** Write the parameter the rule needs: `regex(pattern=^[a-z]+$)`, `max=100`, `lessThan=0`. A pattern genuinely meant to match everything says so explicitly. The contract on [`ParameterizedConstraint`](../validation/contract/constraint.go) now states that the registered instance is a template for `WithParameters` and never a fallback configuration.

### Validation: an answer holds at most 1,000 errors, and a truncated list or map is reported once

**What changed.** One `Validate` call stops at the first error past 1,000, or at the first past 512 KiB spelled in the paths and messages of its answer, and closes its answer with one entry under the root field `""` whose code is `errorLimitExceeded` and whose context holds `maxErrors` and `maxAnswerBytes`, so an answer exhausted by count holds 1,001 entries. The depth cut (`nestingDepthExceeded`) of the members of a list or a map is reported once under the path of the list or the map (`items`), no longer once per element (`items[0]`, `items[1]`, …); a truncated struct field keeps its own entry.

**Symptom.** A client enumerating `validationErrors` may meet the `errorLimitExceeded` entry at the end of a long answer, and a depth-cut entry of a list or a map names the container instead of its elements.

**Remedy.** Treat `errorLimitExceeded` as "more errors exist": show the entries received and ask for the request to be corrected and resent. A client that mapped a depth-cut entry to an element should map it to the container it names.

Two spellings inside an entry are bounded too. A map key past 128 bytes is spelled as its first 128 bytes, cut on a rune, followed by `...(truncated)`, and a path past 256 bytes keeps its tail behind a leading `...(truncated)`. A truncated key or path no longer names its value exactly, and two long keys sharing a 128-byte head spell alike: a client that addresses form fields by the entry's `field` shows an entry carrying `...(truncated)` as an error of the request as a whole. `NewValidator(ValidatorConfig{...})` moves them: `MaxRenderedMapKeyLength` the key cut, `MaxRenderedPathLength` the path cut, beside `MaxErrors` and `MaxAnswerBytes`; 128 and 256 bytes are their defaults.

### Validation: a negative length bound is refused at construction

**What changed.** [`validation.NewMinLength`](../validation/constraint_min_length.go) and [`validation.NewMaxLength`](../validation/constraint_max_length.go) panic on a negative bound, naming it. The tag door (`validate:"min=-1"`) refuses the same value with a parse error rather than a panic, because a tag is data a request path reads while a constructor argument is a declaration.

**Symptom.** A call passing a negative bound — almost always a computed value that came out wrong — now fails at construction instead of returning a constraint.

**Remedy.** Clamp the value at the call site if it can legitimately be negative: `max(0, bound)`. What the refusal replaces is worse than a panic in both directions — `NewMinLength(-1)` built a rule that accepted every value in silence, reading as enforced while validating nothing and leaving no record anywhere, and `NewMaxLength(-1)` built one that refused every value including the empty string with `this field must not exceed -1 characters`, which the client was handed.

### Validation: a non-numeric value under `lessThan`/`greaterThan` answers `notNumeric`

**What changed.** `LessThan` and `GreaterThan` (the `lessThan` and `greaterThan` rules) answer a non-nil value whose kind, behind its pointers, is not numeric — a string, a slice, a struct — with the code `notNumeric` (`ConstraintLessThanErrorNotNumeric`, `ConstraintGreaterThanErrorNotNumeric`), where they answered it with the bound's own code, `greaterThanMax` or `smallerThan`. A nil value still passes, and a nil pointer and a NaN keep the bound's code.

**Symptom.** A validation answer for a non-numeric value under a bound carries `notNumeric` where it carried `greaterThanMax` or `smallerThan`; a numeric value past the bound keeps its code.

**Remedy.** A client, a translation catalogue or a test keyed on the code maps `notNumeric` for these rules, the code the `numeric` rule already answers. `greaterThanMax` and `smallerThan` still cover a numeric value past the bound, a nil pointer and a NaN.

### Validation: a tag that parses to no rule at all is a syntax error

**What changed.** A `validate` tag that survives the empty and skip-marker guards and then yields no rule — `validate:","` is the plain case — reports `invalidRuleSyntax` instead of being accepted as a request to validate nothing. The empty tag and the explicit skip marker `-` are unchanged and still skip the field.

**Symptom.** A field whose tag is malformed in this particular way now rejects every value, where it previously declared validation and enforced none.

**Remedy.** Write the rules the field needs, or write the skip marker `-` if the field is deliberately unvalidated.

### OpenAPI: the generated document follows the repaired validator, in the restrictive direction

**What changed.** The schema mirror in [`openapi/schema.go`](../openapi/schema.go) advertises what the repaired validator actually enforces, so the document generated by `melody:openapi:generate` and served by `SpecificationHandler` changes for every tag class the validation sections above describe. A bare parameterized constraint, an empty `regex` pattern, a malformed or negative `min`/`max` bound and a tag that parses to no rule are advertised as unsatisfiable fields — an impossible facet window, or `not: {}` beside a `$ref` — and such a field is now also listed `required`, because the validator rejects the absent zero value through the same refused rule. The string constraints on a non-string shape are advertised as the refusal they now are: a nullable field accepts exactly `null`, a non-nullable one nothing, and `notBlank` makes any non-string field unconditionally unsatisfiable. The stringified-length reading of `max` on numeric and boolean fields, the `[]byte`/`time.Time` exemptions and
the value-embed brace floor are gone with the behaviour they mirrored. The lockstep is held by an executable proof, [`openapi/schema_test.go`](../openapi/schema_test.go) — the only place `openapi` imports `validation`, test-only — documented in [`OPENAPI.md`](package/OPENAPI.md).

**Symptom.** A published spec regenerated under this release shrinks or contradicts fields whose tags the validator refuses — and a spec-driven client, a contract test or a generated SDK starts failing on those fields at generation or validation time, where the previous document advertised satisfiable bounds the server never honoured and the failures arrived as production 400s instead.

**Remedy.** Treat every newly unsatisfiable field in the regenerated document as the misconfiguration report it is, and fix the tag: give the bare constraint its parameter, move a string rule onto a string field, replace `min`/`max` on numerics with `greaterThan`/`lessThan`. A document with no unsatisfiable fields means every declared rule is one the server can actually enforce.

### Logging: `LogError` anchors the record on the error it was handed

**What changed.** [`LogError`](../logging/logger.go) reads the level, message and context of the error the caller passed in, and reads the already-logged mark at the depth [`exception.MarkLogged`](../exception/utility.go) writes it — the nearest `AlreadyLogged` implementer in the chain. It used to search the chain for the nearest `*exception.Error` for both, which disagreed with the writer on every chain whose markable link is a different type, and anchored the record on whatever exception sat deepest. An error that is not a top-level `*exception.Error` now carries the context of the nearest context provider in its chain plus the cause chain walked from its own wrap link, where it used to be logged with no context at all.

**Symptom.** Records change level and content. An `HttpException` at 500 wrapping an info-level cause used to be filed at info and dropped whole by a production threshold; it is now filed at error with the wrapper's own message. A wrapped-then-marked error stops producing two records. Records for foreign errors gain `cause`, `causeChain` and `causeContextChain`.

**Remedy.** None; tooling that parses log records should expect the richer shape. An alert tuned to the volume of info-level records from wrapped http exceptions will see that volume move to error, which is where those failures always belonged.

### Logging: a record whose level is not one of the five weighs as an error

**What changed.** The json logger gives a record with an unrecognised level the priority of `error` instead of the default lowest priority. The label still carries the raw level, so the record says what it was handed.

**Symptom.** Records that used to vanish under any threshold at or above info now appear at error. The case that produced them is the zero-value `&exception.Error{}` a recovery handler can carry: its level is the empty string, it was filed as debug, and its fatal record was dropped by every production configuration.

**Remedy.** None. A deployment that was unknowingly discarding these records will see them; each one names a value constructed without a level, which is worth finding.

### Logging: the json timestamp carries nanosecond precision in UTC, and its order is the write order

**What changed.** The `time` field is RFC 3339 with the nanosecond field written to its full width and the instant rendered in UTC, rather than `RFC3339`, and the stamp is taken inside the write mutex, so the order of the stamps is the order of the writes. Taken above the lock, the stamp said when the record was FORMED, and the two orders diverged by however long the work between took.

**Symptom.** The `time` field gains a fractional part of exactly nine digits and always ends in `Z`. Whole-second stamps made every record of a busy second indistinguishable; the fraction parses under the RFC 3339 layouts a consumer already uses, so a parser reading `time.RFC3339` is unaffected. A record written in a process whose local zone is not UTC now carries the same instant with the UTC offset rather than the local one.

The full width is what makes the stamps sortable as text, which is the whole of what taking them under the lock buys: `time.RFC3339Nano` trims trailing zeros, so a record landing on a whole second rendered without a fraction at all and sorted after every fractional record of that same second. Rendering in UTC closes the same hole for a zone whose offset moves.

**Remedy.** None for a consumer that parses RFC 3339. A test asserting a whole-second stamp by string equality compares a prefix instead, and one asserting a local-zone offset reads UTC.

### Logging: the json logger reports its own failed write, once, on stderr

**What changed.** When a write to the logger's output fails, the first such failure of that logger's life is echoed to stderr. It is echoed directly rather than through the emergency logger, which is itself a json logger and would re-enter the write that just failed, and it stays silent when this logger's own output already is stderr.

**Symptom.** A new line on stderr — `melody: the json logger failed to write a record to its output: ...` — from a process whose log destination is full, read-only, or on a vanished mount. Previously the entire journal went silent with no signal on any channel and the operator read a healthy-looking empty file.

**Remedy.** None; the echo is once per logger, so a destination that fails on every record does not move the flood from one channel to the other.

### Logging: label maps are copied at every door that takes one

**What changed.** [`NewJsonLogger`](../logging/json_logger.go) and [`NewDefaultLogger`](../logging/default_logger.go), through their config's `Labels`, [`NewLoggingConfiguration`](../logging/logging_config.go) and `LoggingConfiguration.LevelLabels` copy the label map instead of sharing it by reference.

**Symptom.** A caller that built a logger and then mutated the map it still holds no longer changes that logger's labels.

**Remedy.** Build the map before handing it over. What the copy replaces is a fatal concurrent map access no recover reaches: the map is read lock-free on every `Log` call, so a write after construction raced every record being written.

### Logging: a plain-text record is one line, whatever the payload says

**What changed.** The default logger and `LogError`'s process-logger fallback escape the control characters of the message and of the rendered context before writing them.

**Symptom.** A message or a context value containing a line break, a carriage return or an escape sequence renders escaped rather than being obeyed. Text of unknown origin — a header, a form field, a url — regularly reaches these lines.

**Remedy.** None. What the escaping replaces is a record that could be ended early by its own payload, with a fully-formed fake record started after it at whatever level the payload named; the json sibling has the same guarantee from its encoder for the C0 block and the two Unicode line separators, and spells the C1 block, which the encoder leaves raw, as `\u00NN` escapes itself.

### Logging: an exit code outside 1..255 is refused

**What changed.** `LogOnRecoverAndExit` refuses an exit code outside 1..255 with a panic naming the rule, the rule `exception.NewExitError` enforces, applied to the code the handler itself would exit with.

**Symptom.** A program that passed `0` or a code above 255 to the exit handler panics at the call, where the process exited with a code the operating system truncated, or with the success a supervisor read as a clean stop.

**Remedy.** Pass a code in 1..255; a clean stop returns instead of going through the exit handler.

### Logging: `LogOnRecover` only logs

**What changed.** [`LogOnRecover`](../logging/recover.go) no longer terminates the process. A recovered `*exception.ExitError` used to be logged and answered with `os.Exit`, skipping every teardown above the defer; it is now re-panicked unchanged when `panicAgain` is set, so its code reaches the process boundary, and logged and swallowed like any other panic when it is not.

**Symptom.** A goroutine or a command wrapped in `defer logging.LogOnRecover(logger, false)` that panicked with an exit error used to end the process with that code; it now logs the error and returns, and the wrapper carries on as if it had succeeded.

**Remedy.** Where the recovery was meant to be the process boundary, call `LogOnRecoverAndExit` from a deferred function that recovers itself — `defer func() { logging.LogOnRecoverAndExit(logger, recover(), 1, logging.ExitConfig{}) }()` — since it takes the recovered value and `defer logging.LogOnRecoverAndExit(logger, recover(), 1, logging.ExitConfig{})` would evaluate `recover()` when the defer is declared, as nil. Where the panic should keep travelling to the boundary that owns the exit and the teardown, pass `panicAgain` as `true`. Leave `LogOnRecover(logger, false)` where a panic is meant to be logged and absorbed.

### Config and bag: the float string grammar is decimal

**What changed.** The shared parser behind `Parameter.Float` and `bag.Float64` narrows its **string** grammar to plain decimal: an optional sign, digits and at most one decimal point. It used to read `strconv.ParseFloat` in full — underscore spellings (`"1_000.5"`), hexadecimal floats (`"0x1p10"` is `1024`) and exponents (`"1e3"`) all parsed, which is wider than the strict base-10 grammar `Int` reads.

**Symptom.** A configuration or bag value written in one of those spellings now fails the accessor with a refusal naming the decimal grammar, where it previously converted silently. Typed values (`float64`, `float32`, integers) are untouched, and `NaN`/`Inf` remain refused as before.

**Remedy.** Write the value in decimal: `1000.5`, `1024`, `1000`. A value that genuinely needs scientific notation is a computed value, not a configuration literal — compute it, or store the decimal expansion.

### Bag: `Get` answers a copy of the aliasing shapes

**What changed.** [`ParameterBag.Get`](../bag/parameter_bag.go) copies a stored `[]string` or `map[string]string` before answering, at exactly the depth `All` already copied. Every other type is returned as stored.

**Symptom.** A caller that mutated the value `Get` answered — and relied on the mutation reaching the bag — no longer writes through: the bag keeps its own value. That reliance was a data race with any concurrent reader and is exactly what the copy exists to prevent. Callers that treat the answer as read-only, which is what the documentation prescribed, see one allocation per slice- or map-valued `Get` and nothing else.

**Remedy.** Mutate through the writing doors: `Set` replaces a value, `AppendString` appends under one critical section.

### Cache: the manager no longer closes a backend it was handed

**What changed.** [`NewManager`](../cache/manager.go) builds a manager that does not own its backend: `Close` leaves the backend open, because on the container path both are registered services and the container closes each one itself — the cascade closed the backend twice, which a backend wrapping a connection typically reports as a failure on the second call. `ManagerConfig{OwnsBackend: true}` keeps the cascade for the caller that builds both by hand.

**Symptom.** A manager built directly with `NewManager` over a hand-built backend no longer stops that backend's cleanup goroutine on `Close`; the backend outlives the manager until its own `Close`.

**Remedy.** A caller that builds both by hand and wants one `Close` to end both passes `cache.ManagerConfig{OwnsBackend: true}`. The container path needs no action.

### Cache: a closed in-memory backend refuses every operation

**What changed.** [`InMemoryBackend`](../cache/in_memory.go) carries a closed flag: after `Close`, every operation answers `cache backend is closed` instead of silently succeeding against a map whose cleanup goroutine is gone — an entry written after `Close` was never reclaimed by anything and grew the map for the rest of the process. `Close` itself stays idempotent.

**Symptom.** A write or read that races a shutdown past the backend's `Close` receives an error instead of a silent success that nothing would ever sweep.

**Remedy.** None for the ordinary lifecycle. Code that deliberately used a closed backend as a plain map keeps a backend it does not close, or its own map.

### Cache: the in-memory backend refuses what it silently accepted

**What changed.** Four degenerate inputs that used to be absorbed are refused. The empty key answers `cache key is empty` on every operation — it used to be a real key, which the `rueidis` backend refuses, so every caller whose key came up empty silently shared one entry until the deployment switched backends. A negative ttl on `Set`/`SetMultiple` is refused — it used to store an immortal entry, the exact opposite of a ttl computed from an already-passed deadline; no expiry is written `bound.UnlimitedDuration()`. `Increment`/`Decrement` on an existing empty or blank payload answer `cache value is not a valid int64` the way any textual payload does — it used to be adopted as a zero counter and overwritten, destroying the entry, where redis `INCRBY` errors. A `MaxItems` below 1 panics at construction — a negative one used to silently mean unbounded, disarming eviction the operator believed was armed.

**Symptom.** Each refused input surfaces as an error (or, for `MaxItems`, a boot-time panic) at the call that produced it, instead of a silently wrong cache.

**Remedy.** Fix the calling code: supply a non-empty key; when a ttl computed from a deadline comes out negative, the deadline has passed, so skip the write (or `Delete` the key) rather than clamping it at zero, which is refused like any ttl under a second — no expiry is the separate, explicit `bound.UnlimitedDuration()`; keep counter keys away from non-counter writes; and pass `MaxItems: bound.UnlimitedCount()` for an explicitly unbounded backend.

### Cache: a counter payload is read on the redis integer grammar

**What changed.** `Increment` and `Decrement` on the in-memory backend parse an existing payload on the grammar redis `INCRBY` accepts, not Go's lenient one: an optional minus, then a first digit of 1-9 unless the whole number is the single digit `0`, and nothing else.

**Symptom.** A counter whose stored payload is `+1`, `01`, `-0` or digits with surrounding whitespace answers `cache value is not a valid int64` where it used to be read as a number and overwritten.

**Remedy.** Write counters through `Increment`/`Decrement` only. A payload written by other code, or migrated from elsewhere, is rewritten in the canonical decimal form (`strconv.FormatInt`) — no sign but a leading minus, no leading zero, no `-0`, no whitespace — which both backends read alike.

### Cache: the key grammar is the contract's, on both backends

**What changed.** `cachecontract.Backend` now states the key grammar — non-empty, no spaces, no newlines, at most 1024 bytes — and the in-memory backend enforces it with the same refusals the redis backend always answered. The two implementations of one promise refused different keys.

**Symptom.** A key carrying a space or a newline — typically built from user input — now fails every operation against the in-memory backend with `cache key contains spaces`/`cache key contains newlines`, where it silently worked in development and failed only in production.

**Remedy.** Sanitize the key at the call site (hash it, or strip the whitespace). The refusal names the key.

### Cache: a payload the serializer cannot decode is a typed miss

**What changed.** [`Manager.Get`](../cache/manager.go) wraps a deserialization failure in the new [`DeserializationError`](../cache/deserialization_error.go), and [`Remember`](../cache/remember.go) treats that type as a miss: the callback recomputes and its `Set` overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever. [`Manager.Many`](../cache/manager.go) no longer discards the whole answer over one corrupt entry: the entry is left out the way an absent key is, and the error returned beside the good values is a `DeserializationError` naming the culprit keys deterministically. Every other error keeps meaning the cache itself failed.

**Symptom.** A key whose payload was corrupted out-of-band recovers on the next `Remember` instead of erroring forever; `Many` over a corrupt entry returns the good values plus a typed error naming the bad keys, where it used to return nothing and an anonymous encoding error.

**Remedy.** None for most callers. A caller of `Many` that treated any error as "no values" now also has the partial map available; a custom `Cache` implementation that wants the same self-healing under `Remember` wraps its own deserialization failures with `NewDeserializationError`.

### Cache: `Remember` answers one shape on the miss and on every hit

**What changed.** The computing call of [`Remember`](../cache/remember.go) returns the value passed through the manager's serializer round-trip — one local encode and decode, no backend involved — which is the exact shape every cached call answers. Until now one key had two shapes: the callback's own value on the miss, the decoded generic form on every hit, so a type assertion worked on the cold path and failed on the warm one, from the second call on. A `Cache` implementation that does not expose its stored shape answers the callback's value unchanged.

**Symptom.** A callback returning `int` is answered as `float64` on the FIRST call too, and a struct as `map[string]any`, where the miss used to answer the callback's own types. The cost is uniform rather than deferred: an integer beyond 2^53 comes back changed on the computing call as well.

**Remedy.** Type-assert against the decoded shape, or decode into a typed target. Where a cached value carries an integer that large, carry a version inside it or key it so it never decodes through JSON.

### Cache: a value-kind Cache no longer coalesces in Remember

**What changed.** [`Remember`](../cache/remember.go) coalesces concurrent callers under one in-flight computation only for pointer-kind `Cache` implementations, whose address tells instances apart. A value-kind implementation used to share one flight per type: two different instances with different backends collapsed onto one leader, and the second caller received — and cached nothing from — the value computed for somebody else's cache. A value-kind instance now runs every callback itself.

**Symptom.** A struct- or map-kind `Cache` implementation loses stampede deduplication and never a correct answer.

**Remedy.** Implement `Cache` on a pointer receiver to regain coalescing.

### Cache: a recovered panic carries its cause

**What changed.** The recovery boundaries of [`cache.Remember`](../cache/remember.go) hand the panic value on as the CAUSE of the error they fabricate, and capture the stack of the goroutine that raised it. The `panic` context key they already wrote is unchanged; `panicStack` is added beside it. A panic raised by the cache itself — the backend or the serializer, on the leader's own reads and writes — is now reported as `cache remember cache access panicked` rather than being blamed on the callback, which the wrapper recovers separately.

**Symptom.** `errors.Is` and `errors.As` on the returned error now reach the failure underneath, where before they stopped at the fabricated wrapper. A diagnosis that followed `cache remember callback panicked` into a callback that never panicked now names the cache access instead.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked callback the same way.

### Cache: the default Remember flight is cancelable

**What changed.** [`Remember`](../cache/remember.go) arms a cancelable flight unless `RememberConfig.Detached` is set, which the third major kept as the opt-in `WithCancelable(true)` beside its released detached default. Under the detached flight a hung callback owned its key for the life of the process: the leader could not be told to stop, the entry was deleted only by the leader's own return, and the one path that replaces a live entry requires a canceled flight, which a detached one never becomes. Now the last waiter leaving cancels the flight's context, and the next caller of the key finds the canceled flight, replaces it, and leads a fresh computation.

**Symptom.** A callback that outlives all its waiters receives a canceled context and is recomputed by the next caller, where it kept running detached with its eventual result written into a record nothing reads; a callback that ignores its context changes nothing for itself, but its key stops being pinned once its waiters are gone. A `WaitTimeout` **shorter than the callback** never stores: the lone waiter's timeout leaves the flight without waiters, the flight is canceled, the callback that honours its context returns the cancellation, and every later call leads a fresh flight to the same end, so the key is never populated where the detached computation populated it once it finished. Callers on the detached flight and on the cancelable one never share a flight — the single-flight unit keys on cancelability — so nothing coalesces differently.

**Remedy.** A callback whose computation must survive the departure of every caller, and a bounded wait on a slow callback, ask for the detached flight explicitly: `RememberConfig{Detached: true}`, or give the callback its own deadline instead of the waiters. Long callbacks still want their own deadlines — the default bounds the computation's ownership of the key, and the wait, 30s unless `WaitTimeout` says otherwise.

### Serializer: a refusal of one type is not a refusal of the negotiation

**What changed.** `Accept: application/json;q=0` against a manager that also holds `text/plain` is answered with plain text rather than `406 Not Acceptable`. A type covered by a `q=0` range is recorded as refused and can never be served — by the negotiation or by the fallback, which steps past json when json is what was refused — and `ErrNotAcceptable` is answered only when every registered type is refused.

**Symptom.** A request whose accept header names one type it does not want, against an application with more than one serializer registered, receives a representation where it used to receive a 406.

**Remedy.** None: this is what the manager's own comment and `SERIALIZER.md` promised. A client that wants nothing at all still gets its 406 by refusing everything — `*/*;q=0`.

### Serializer: a manager without json answers with its own first serializer

**What changed.** A manager deliberately configured without `application/json` answers an empty or unmatched accept header with its first configured serializer in lexical MIME order. It used to refuse with `no default serializer configured` while a serializer sat beside the refusal, and the http result handler, finding no `service.serializer` registered, answered that refusal with hardcoded json — the representation the configuration had excluded.

**Symptom.** A controller result requested without an accept header, or with one naming no configured type, is served by the first configured serializer, under its content type, where it was served as `application/json`.

**Remedy.** None when json was excluded on purpose. An application that relied on json being served anyway registers `application/json` in its manager's map; json is then the default again.

### Serializer: the accept header is read on the one strict grammar

**What changed.** The manager's own header parser reads `q` through [`internal.ParseQualityValue`](../internal/quality_value.go) and splits members and parameters through [`internal.SplitOutsideQuotes`](../internal/split_outside_quotes.go), the grammar the http readers already shared. A `q` outside the RFC 7231 qvalue grammar — digits past the third decimal are tolerated when they are zeros — drops its whole member instead of being scored as full acceptance, clamped into a fabricated refusal, or carried through as a `NaN` weight no comparison could select or refuse; and a comma or semicolon inside a quoted parameter value stays inside its member, so the `q=0` in `text/plain;version="1,2";q=0` keeps covering the type it names instead of being detached from it.

**Symptom.** `q=1e-1` and `q=1.5`, which used to be accepted, now drop their member. A header whose every member is malformed matches no candidate and is answered with the default representation by the manager itself, no refusal raised; only a header holding more members than the negotiation reads is refused whole and answered `406`.

**Remedy.** Spell `q` within the RFC grammar — a zero with up to three decimal digits, or a one with up to three zero decimals.

### Serializer: two mime keys that normalize to one are refused at construction

**What changed.** `NewSerializerManager` refuses a map in which two spellings collapse onto one normalized key — `application/json` and `Application/JSON; charset=x` — naming the normalized key and both spellings in sorted order. The overwrite used to be silent, with map iteration order deciding which serializer survived, so the winner could change from one boot to the next and the loser vanished. A typed-nil serializer instance is refused exactly like the untyped nil, at construction rather than on the first request the negotiation routes to it.

**Symptom.** An application whose serializer map carries two spellings of one media type fails to build its manager instead of booting with an arbitrary winner.

**Remedy.** Register each media type once, under any spelling.

### Serializer: a plain-text payload is never aliased into the caller's buffer

**What changed.** `PlainTextSerializer.Serialize([]byte)` returns a copy rather than the caller's backing array, and `Deserialize` into a `*[]byte` stores a copy rather than the payload slice itself. A typed-nil pointer target is refused with the same error the untyped nil gets, where it used to pass the plain comparison, match its case and dereference nil on the assignment.

**Symptom.** None for a caller that did not rely on the aliasing. A caller returning a pooled buffer after `Serialize` no longer overwrites the bytes it believed it had snapshotted.

**Remedy.** None.

### Clock: `FrozenClock.Advance` refuses a negative duration

**What changed.** `Advance` accepted a negative duration and silently moved the frozen clock backwards, breaking the monotonic invariants the code under test relies on. It now panics with `invalid advance duration`; zero remains a no-op.

**Symptom.** A test advancing by a negative duration panics at the call site.

**Remedy.** Use `TravelTo`, the deliberate door for backwards motion.

### Clock and runtime: the nil refusals read through the interface

**What changed.** `ClockMustFromResolver`, the `runtime` constructors and the `runtime` resolvers judge their arguments through the interface rather than with a plain `nil ==` comparison, so a typed nil is refused by name at the door it was handed to. `selectRuntimeResolver` reads the same way: a custom `Runtime` whose `Scope()` yields a typed nil is refused, a runtime resolving through its scope alone.

**Symptom.** A wiring mistake that used to surface as a nil dereference inside the container package — on the request path, with the panic blaming the container — is now refused where it was made, naming the argument.

**Remedy.** None.

### Event: `AddSubscriber` answers the registration and `RemoveSubscriber` takes it

**What changed.** `event/contract.EventDispatcher` declares `AddSubscriber(subscriber) SubscriberRegistration` and `RemoveSubscriber(registration SubscriberRegistration) int`: an installation is filed under an `event/contract.SubscriberRegistration` the dispatcher issues, and removed through it. The third major kept the released pair filed under the subscriber's pointer, refused a second installation of one pointer and a value subscriber there, and carried the registration form on the optional `event/contract.SubscriberRegistrar` as `AddSubscriberWithRegistration` and `RemoveSubscriberRegistration`; the sibling and its two methods are removed. The dispatchers keep no reference to an installed subscriber of their own, only the listeners they installed, which keep it reachable when they are its methods.

**Symptom.** A call that removes a subscriber by its value, `RemoveSubscriber(subscriber)`, a call to `AddSubscriberWithRegistration` or `RemoveSubscriberRegistration`, an assertion on `SubscriberRegistrar`, and an out-of-tree `EventDispatcher` with the released pair no longer compile. A second installation of one subscriber, or a value subscriber, installs instead of panicking.

**Remedy.** Keep the registration `AddSubscriber` answers where the subscriber is removed later, and hand it to `RemoveSubscriber`; a subscriber installed for the life of the process ignores it:

```go
registration := eventDispatcher.AddSubscriber(subscriber)
// later
removedCount := eventDispatcher.RemoveSubscriber(registration)
```

A code path that asserted `SubscriberRegistrar` calls the two methods on the dispatcher. A dispatcher of your own answers a registration from `AddSubscriber` and removes by it.

**Why a registration.** A subscriber's pointer is not always an identity: a subscriber struct that carries no fields occupies no memory, and every zero-size allocation in Go answers one address, so two instances of such a type are one pointer, and removing one by pointer would take the other's listeners down. The registration is an id the dispatcher issues, so two installations of one subscriber are removed independently and a value subscriber installs like any other.

### Event: a dispatch that skipped a required listener fails closed, with a type you can assert

**What changed.** When a listener ends a dispatch early — by stopping propagation or by failing — and a listener registered required sits behind it, the dispatch returns an `*event.RequiredListenerSkippedError` instead of completing, or instead of returning only the ending listener's own failure. The http kernel refuses such a dispatch on `kernel.request` and `kernel.controller` even when a listener produced a response, dropping that response for the error page.

**Symptom.** A listener that stops propagation and answers the request at a priority ahead of a required listener — an http cache above the access-control listener is the shape this was found in — now yields a 500 error page where it used to deliver its own response. A dispatch that used to complete quietly now returns an error.

**Remedy.** This is the guarantee working; the previous behaviour served pages with access control never consulted. If the listener is legitimately entitled to short-circuit past required listeners, register it with `eventcontract.ListenerConfig{Failure: eventcontract.ListenerFailureConfig{MaySkipRequiredListeners: true}}`. Both default off, so a dispatcher whose listeners declare nothing behaves exactly as before. Assert the type on the error a dispatch returns DIRECTLY rather than through `errors.As`: a listener may dispatch further events, and a nested dispatch skipping a required listener of its own travels up as the cause of an ordinary listener error, which is a different and wider policy.

### Event: a panic the dispatcher recovers carries its value as the cause

**What changed.** The dispatcher's outer recovery, the one that describes a panic as `event dispatch panicked`, carries an error-shaped panic value as the error's cause through `exception.PanicCause`, as a listener's recovery already did; a typed-nil error value is not taken as a cause.

**Symptom.** An `*exception.HttpException` panicked from an event's own method answers its own status, where the request was answered 500; `errors.Is` and `errors.As` on the dispatch error reach the panicked error; its context and cause chain reach the journal record.

**Remedy.** None for an application that only logs the failure. One that classified a failed dispatch as a 500 whatever was panicked, or matched on the error alone, expects the panicked value's own status and chain.

### Event and debug: subscriber declarations are validated whole, and `debug:events --verbose` changes its json shape

**What changed.** `AddSubscriber` validates every subscribed event before registering a single listener, and refuses a subscriber that declares no events as well as an event name mapped to an empty list. Under `--verbose`, the json document of `debug:events` carries `data.{events, listeners}` where it carried the event list alone; without `--verbose` the shape is unchanged.

**Symptom.** A subscriber whose declaration is partly malformed now panics before anything is installed, where it used to install the valid part and then panic. A subscriber assembled from configuration that produced an empty declaration now fails the boot instead of registering nothing and reporting success. A machine consumer reading `data.items` from `debug:events --format=json --verbose` finds it under `data.events.items`.

**Remedy.** Fix the declaration — the half-installed subscriber was the defect: its first event's listeners were live and firing under a subscriber the caller had been told was refused. For the json consumer, read `data.events.items` under `--verbose`, or drop the flag; the listener detail, including the marks that say whether the fail-closed guarantee is armed, is now reachable from json at all, which it was not before.

### Cli: `NewCommandContext` no longer exits inside `Run`

**What changed.** [`cli.NewCommandContext`](../cli/command.go) installs an inert `ExitErrHandler`: the engine's default called `os.Exit` from inside `Run` on any error a command returned, skipping the application's teardown, and an error now returns from `Run` to its caller, an exit-coded one included.

**Symptom.** Code that drives the root command itself — a test, or a binary calling `Run` without `Application` — sees `Run` return the error where the process used to end inside it with the error's code.

**Remedy.** Check the error `Run` returns and choose the exit code after the cleanup, which is what `Application` does. A caller that wants the engine's own exit sets `ExitErrHandler` back on the context it was handed.

### Cli: a failing command's `[error]` line goes to the error stream, in every format

**What changed.** The `[error]` line of a failing command is written to the command's `ErrWriter`, standard error by default, whatever the format, where it ended the document on the output stream and `--format=json` wrote none.

**Symptom.** A redirected or piped standard output no longer ends with the `[error]` line; under `--format=json` a failing command now prints the line, on standard error.

**Remedy.** A script that read the failure off standard output reads standard error, or the exit code, and keeps the two streams apart so the line never lands inside a json document. A test or an application that captures the error stream sets it through `RootConfig.ErrorWriter`, or `ErrWriter` on the command.

### Debug: `NewMiddlewareCommand` takes the description and the build providers

**What changed.** `debug.NewMiddlewareCommand` takes two channels: `MiddlewareDescriptionProvider`, which reports the pipeline as the selection and the ordering see it with no factory run, and `MiddlewareBuildProvider`, which produces the built chain; the framework's own wiring uses it. The one-provider form over `MiddlewareProvider`, and the name `NewMiddlewareCommandWithProviders` the two-provider form carried on the third major, are removed. A nil provider is refused by panic at construction, and a zero-value command answers a named refusal instead of calling a nil function.

**Symptom.** A hand-wired command built with `NewMiddlewareCommand` over a `MiddlewareProvider`, or with `NewMiddlewareCommandWithProviders`, no longer compiles. An application that lets `Application.bootCli` register the debug family sees no change.

**Remedy.** Rename `NewMiddlewareCommandWithProviders` to `NewMiddlewareCommand`. A command that handed in one provider hands in the two the application already has:

```go
debug.NewMiddlewareCommand(
    func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
        return myPipeline.Describe(kernelInstance.Environment(), group)
    },
    func() ([]httpcontract.Middleware, error) {
        middlewares, _, buildErr := myPipeline.Build(kernelInstance, group)

        return middlewares, buildErr
    },
)
```

### Debug: `debug:container` and `debug:middleware` describe by default and build under `--build`

**What changed.** The bare `debug:container` listing runs no provider: it reports names, lifetimes, built state and the provider's declared return type out of the registration records. The resolve sweep that builds services sits behind a new `--build` flag; naming a single service still resolves that one. The bare `debug:middleware` listing describes the pipeline, with the inactive entries carrying their reasons, and `--build` runs the real factories under a recover. The `--build` sweep reports its failures on the envelope, with `error.code = "debug.buildFailed"`, the failure count and the failed names in the details, and the first failure as the cause.

**Symptom.** A script that ran `app debug:container` as a smoke test — relying on the listing to construct every service and exit non-zero when one did not — now sees it pass, because the listing no longer builds anything. The listing itself is faster and no longer opens connections.

**Remedy.** Add the flag where the building was the point: `app debug:container --build --format=json || exit 1`. Note that the `debug:*` family is registered on the development environment alone, `debug:router` excepted, so on a deployed application the invocation falls to the unknown-command path and exits `2` with `cli command not found` — a `|| exit 1` gate fires there for the wrong reason. A gate that must exercise the container has to run against a process booted in the development environment.

**Why the default could not stay.** `Get` was called on every name in the container, so listing a production wiring dialled every pool, opened every connection and built every singleton as the price of a table — the introspection door exists precisely so that listing a container does not mean building it.

### Debug: `debug:router` reports the two discriminators the dispatch breaks ties on

**What changed.** Every `debug:router` row carries `priority` and `order`, in the table and in the json document, and under `--verbose` also `requirements`, `defaults` and `attributes`. The listing's comparator is total: equal pattern and methods are broken by registration order.

**Symptom.** A machine consumer reading `data.items` finds four new fields per item, and two more under `--verbose`. A table consumer parsing by column position sees `priority` and `order` inserted after `name`. Rows that used to flip between runs now hold still.

**Remedy.** Read the fields by name. The additions are what makes the command answerable: priority and registration order are the two discriminators the dispatch itself uses, so two routes overlapping on pattern and methods rendered identically and the command that exists to say which one answers could not say it.

### Debug: `debug:version` reads the version rows off the envelope meta

**What changed.** The application row reads `envelope.Meta.Version.Application`, the version the application declares in `ApplicationConfig.Version`.

**Symptom.** The `application` row and the json document carry the declared version, where they used to carry an empty value.

**Remedy.** None; the row reports what the application declared.

### Container: a scoped registration under the protected `service.` prefix is refused at boot

**What changed.** Both scoped registration doors — `Container.RegisterScoped` and `Scope.RegisterScoped`, and the generic helpers over them — refuse a service name beginning with `service.`, with or without `ReplacesContainerService`.

**Symptom.** A boot that used to succeed now panics with `service is protected and cannot be registered as a scoped service`, naming the service. The wiring generator produces the same refusal for a constructor carrying `//melody:scoped` under such a name.

**Remedy.** Rename the scoped service out of the reserved namespace. The prefix is the framework's own, and the override path has always refused to substitute a `service.` name for a reason: a scoped registration of that name performs exactly that substitution, inside every scope the kernel resolves through, for the whole life of the process — the protected singleton is replaced once per request with nothing refused anywhere. A CONTAINER-lifetime registration of a `service.`-prefixed name is unaffected, so only the scoped ones have to move.

### Container: `Has` and `HasType` answer under the same suspension `Get` enforces

**What changed.** On the resolver a provider is handed, `Has` and `HasType` consult the request scope only when the resolution may read it. A provider the CONTAINER owns runs with the scope suspended, so it now hears `false` for a name only the scope carries — the same answer its `Get` gives.

**Symptom.** A container-lifetime provider that branches on `resolver.Has(name)` for a scope-only service takes the other branch. Where it previously went on to `MustGet` the name it had just been told existed, it no longer panics.

**Remedy.** Usually nothing: the previous answer was wrong in the dangerous direction. A container provider that genuinely needs a per-request value must not read it at construction — a process-lifetime singleton assembled out of one request's values holds that request for the life of the process. Take the value per call through the runtime or the request's own resolver instead, or make the service scoped.

### Container: `Scope.RegisterScoped` refuses a closed container, and a scope override must fit the types its name is registered under

**What changed.** Three refusals move earlier. `Scope.RegisterScoped` refuses once the container has begun closing, the answer `Container.Register` and `Container.RegisterScoped` already gave. A scope override propagates to every type its name is registered under — the container's registrations, the plan's and the scope's own — and is refused before anything is written when the value does not fit one of them, the rule the container-level door already applied.

**Symptom.** A registration made during shutdown fails where it used to report success. `Scope.OverrideInstance` / `OverrideProtectedInstance` returns `override value is not assignable to the registered service type` for a value that used to install; a test that substituted an unrelated implementation under a name registered under a concrete type is the usual case.

**Remedy.** For the override, register the name under the interface the substitute also satisfies — the guard admits raw assignability for an interface registration — or opt the registration out of the type with `RegisterOptions{TypeRegistrationDisabled: true}`, which leaves the name with no registered type to fit. The refusal is what stops the name and the type maps from learning two different answers, which is what a type-keyed resolution then served.

### Container: a lazy handle over a request scope becomes terminal when that scope closes

**What changed.** `container.Lazy` and `container.LazyByType` ask the resolver they captured whether it has closed. The first closed answer makes the handle terminal: the scope-is-closed error on that call and on every later one, with the memoized value, the closure and the resolver dropped. The resolver's `Closed()` or `IsClosed()` is the door the question is asked through; a resolver that carries neither is read as open.

**Symptom.** A handle built over a request scope and used after the request stops answering the memoized value and returns `lazy service scope is closed`. `LazyService.Get(runtimeInstance)` panics with that error rather than handing back a dead request's state; the runtime handed does not change the answer. A handle built over the boot scope answers it once the boot finished with `ErrBootFinished` matched as well.

**Remedy.** Build the handle over the container where it is meant to outlive the request — every container resolution mints a fresh resolver context, so that is also the form safe for concurrent first uses. Code shared across requests resolves per call through `FromResolver` with the current request's resolver; the value is then keyed to the right scope by the scope's own instance map. A handle built over the container, or over the resolver a container provider was handed, follows the container instead: it keeps serving through the teardown and turns terminal once the teardown finished, answering `ErrContainerClosed` rather than the scope's sentinel, so a caller classifying the refusal tells an application shutting down from a request that ended.

### Http: `RedirectResponse` is relative-only

**What changed.** [`RedirectResponse`](../http/response.go) — and `RedirectFound`/`RedirectMovedPermanently` through it — refuses by panic a location that leaves the application: one carrying a scheme (`https:`, `mailto:`, `javascript:`), a scheme-relative `//host`, or a backslash, which several browsers fold to a slash while `net/url` does not. [`RedirectExternalResponse`](../http/response.go) is the new unguarded form.

**Symptom.** A handler that redirected to an absolute url through `RedirectResponse` now panics — caught by the kernel's recovery into a 500 — naming the door to use. A handler passing relative locations sees no change.

**Remedy.** A redirect to a fixed or allowlisted external target moves to `RedirectExternalResponse`, whose name is the assertion that the target is the caller's own. A redirect whose target arrives from client input must not become external by remedy: validate it against an allowlist first, or keep it relative.

### Http: the router's registration doors are boot-only and refuse after serving starts

**What changed.** Every route registration door refuses by name once `Kernel.ServeHttp` has built the handler; the kernel has no configuration door after `NewKernel` (see "Http: the kernel is configured through `ApplicationConfig.HttpKernel`" above). The contract sentence is written on `httpcontract.Router` and `httpcontract.RouteHandler`.

**Symptom.** An application that registers a route lazily — from inside a handler, from a goroutine started after boot, from a hot-reload path — panics with `may not register a route after the http kernel started serving`, naming the door.

**Remedy.** Move the call into the composition root or a module's registration hook, which run before the handler is built. There is no opt-out, and that is deliberate: every one of those doors writes state each request goroutine reads without synchronization, so what used to happen instead was a data race — and for the route tree's maps a concurrent write is an unrecoverable fatal error the process cannot recover from. The READING doors are untouched: `RouteDefinitions`, `RouteDefinition`, `RouteRegistry` and the introspection surface stay open for the life of the process, because the openapi document and the route manifest are served from handlers.

### Http: the kernel's request attributes are published after the route's, and the session is `_session`

**What changed.** The kernel publishes the request attributes it owns — the session, under `_session`, and the resolved scheme, `_scheme` — after the route attributes, so a route attribute of the same name no longer replaces them. The session's key takes the framework's underscore prefix, `_session`, which the third major kept at its released value `session`, so every attribute the kernel owns carries the prefix, and it is private: the session is read through `http.RequestSession`.

**Symptom.** A handler that reads the session under the literal key `session` finds nothing there. A route declared with an attribute named `_session` or `_scheme` no longer reaches the handler under that name: the handler reads the kernel's session and scheme there. A route attribute named `session` reaches the handler again, as an application attribute.

**Remedy.** Read the session through `http.RequestSession`, never through the literal key, and rename a route attribute that collides with `_session` or `_scheme`. A handler that rotates the session publishes the rotated one through `http.RegenerateRequestSession`, and the scheme follows the request and the kernel's `KernelConfig.ForwardedHeadersPolicy`, not a route.

### Http: `SetCookie` refuses a cookie name that is not a token

**What changed.** `SetCookie` panics on a cookie name that is not a valid HTTP token, as it did on an empty one, where the standard library serialized it as the empty string and an empty `Set-Cookie` header went out.

**Symptom.** A call with a name carrying a space, a separator or a control character panics with `the cookie name is invalid and can not be set` where the response silently carried no cookie.

**Remedy.** Use a token name: letters, digits and ``!#$%&'*+-.^_`|~``. A name built from client input is validated before it reaches `SetCookie`.

### Http: the compression middleware reads level 0 as the default

**What changed.** `CompressionMiddleware` reads level 0, gzip's `NoCompression`, as `gzip.DefaultCompression`: level 0 labelled a stored body gzip and grew it by the framing. The configuration is copied, not rewritten. The fourth major refuses a level outside -2..9 naming `CompressionConfig.Level`, where the third major read it as the default too; -1 selects the default explicitly.

**Symptom.** A deployment that configured level 0 to turn compression off now compresses at the default level.

**Remedy.** To serve uncompressed, do not install the middleware, or exclude the paths or content types through `CompressionConfig`. To choose a level, pass one from `gzip.HuffmanOnly` (-2) to `gzip.BestCompression` (9), other than 0.

### Http: routes match the path as the client spelled it, and hosts fold case

**What changed.** Matching reads the path as the client spelled it — `URL.RawPath` while it still unescapes to `URL.Path`, `URL.EscapedPath` when no `RawPath` was kept, and a `RawPath` that no longer unescapes to `URL.Path` refused with `400` before any match — and unescapes each segment after the split, so `%2F` stays inside a segment instead of separating two. `matchesHost` folds case and ignores the port unless the route declared one.

**Symptom.** A request to `/admin%2Fusers` no longer reaches the `/admin/users` handler; it reaches a single-segment route, binding `admin/users`. A route bound to `example.com` now also matches `Example.com` and `example.com:8443` — which is what makes it reachable in local development at all.

**Remedy.** Usually nothing. If an application relied on the decoded spelling reaching a deeper route, register the route the client actually addresses. A route that must discriminate on a port declares it: `example.com:8443` still matches only that port. Note that the url generator already refused a `/` inside a parameter value for exactly this reason, so no generated url changes.

### Http: a route declaration that contradicts itself is refused at registration

**What changed.** Four registration refusals join the ones already there: a pattern segment written as `{id}` rather than `:id`; a route declaring `Locales` whose pattern and defaults supply no `_locale`; a route whose pattern carries `:_locale` while declaring no `Locales` list; and route defaults now merge before the locale gate reads them.

**Symptom.** A boot that used to succeed panics by name. Each refused shape produced a route that was silently unreachable or silently unvalidated: the brace pattern matched only the literal url `/users/%7Bid%7D` and bound nothing; `Locales` without a locale parameter could never match anything; and `:_locale` without a list bound whatever the client sent and the kernel published it verbatim as the request locale.

**Remedy.** Write `:id` instead of `{id}`. Give a localized route either a `:_locale` segment or a `_locale` default, and declare the `Locales` list beside it — a route whose locale comes from a default is now reachable, which it was not before.

### Http: a `RouteOptions` value reused across two grouped routes is refused

**What changed.** A `RouteGroup` no longer writes its name prefix and its merged requirements and defaults back into the caller's `RouteOptions`, so one options value reused for two routes in the same group registers the second under the documented duplicate-name refusal instead of under a doubled prefix.

**Symptom.** A boot that reused one named options value across two grouped registrations is refused: the boot report `duplicate registrations detected at boot` lists the second route as `httpRouteName "<name>"` (a router driven outside the boot answers `route name already exists`), where it used to register the second route under a name nothing could generate. A reused value that carries no name registers both routes, as before.

**Remedy.** Give each grouped route its own `RouteOptions` value, or a distinct name.

### Http: the access log redacts query values

**What changed.** The access-log record and the kernel's 405 and no-route records keep the query parameter names and replace every value with `xxxxx`.

**Symptom.** Log lines that used to read `"query": "token=abc123"` now read `"query": "token=xxxxx"`. A query that does not parse is redacted whole.

**Remedy.** Nothing to change. Any log pipeline matching on a query VALUE has to stop; matching on parameter names still works. This is also a patch-level fix on v1 and v2, because a credential in a url was being copied into the journal on every request. The `Referer` field is redacted the same way — its parameter names kept, every value `xxxxx`, its fragment and user information dropped — so a pipeline matching on a Referer value has to stop too.

### Example: the event routes are behind the firewall

**What changed.** In the reference application, `/events/publish` is a POST behind `RoleEditor` and `/events/stream` is behind `RoleUser`, with the subscribed topic authorized against the caller.

**Symptom.** `GET /events/publish?topic=…&text=…` no longer works; it is a POST and it requires an editor. `GET /events/stream` requires an authenticated caller, and the catalog topic requires `RoleEditor`.

**Remedy.** This is a change to the reference application, not to the framework, and it is here because the shape it replaced is one an integrator may have copied: a public `^/events` rule let an unauthenticated GET inject a frame into every stream open across the cluster — reachable cross-site from an `<img>` tag — and let an anonymous reader watch the product and user writes made behind `RoleEditor` and `RoleAdmin` go by in real time.

### Example: the object storage reads `S3_INSECURE`

**What changed.** The reference application dials its object storage over https unless `S3_INSECURE` is exactly `true`, in place of `S3_SECURE`; the meaning of the boolean is inverted, so an omitted key never downgrades the transport.

**Symptom.** A `.env` that carried `S3_SECURE=false` for a plain-http endpoint is read as secure until the key is renamed, and the storage tries https.

**Remedy.** Replace `S3_SECURE=false` with `S3_INSECURE=true`; against an https endpoint drop the key.

### Example: the message bus and the outbox relay need `AMQP_DSN`

**What changed.** Without `AMQP_DSN`, `/messagebus/dispatch` and `/outbox/relay` answer 503 naming the key and `melody:outbox:relay` refuses naming it, where they published into a process-local queue nothing read.

**Symptom.** A deployment without a broker that called those doors gets the 503 or the refusal; the outbox keeps its rows until a broker is configured.

**Remedy.** Set `AMQP_DSN` before using them. `messagebus:dispatch` keeps its in-process round trip, which needs no broker.

### Example: the history door answers the current reading beside the archive

**What changed.** The history door answers, beside `readings` and `limit`, a `current` object — `taken_at`, `headline`, `payload`, `from_cache` — holding the reading the scheduled refresh cached, or one taken on a cold cache. A reading is cached for the refresh interval, at least the schedule's hour and five minutes, where it lapsed unread after five minutes.

**Symptom.** The payload carries one more key, and a request on a cold cache takes a reading.

**Remedy.** A client reads the archive under `readings` and the latest reading under `current`; `from_cache` says whether it was taken for this request.

### Example: the catalogue database is opened at its first use

**What changed.** The catalogue database is opened at its first use rather than at boot, as the archive is: a process that never reads it, one serving only `/health` included, opens no connection, and a declared catalogue that cannot be opened is refused, named with its location, by the first request or command reading it, where the boot refused it.

**Symptom.** A deployment whose catalogue cannot be reached boots and passes `/health`; the first catalogue request or command fails.

**Remedy.** A readiness check that must prove the catalogue reads through it — a catalogue request, or a command that reads it — rather than relying on the boot or `/health`.

### Example: the schema migration is amended, and an earlier volume is rebuilt

**What changed.** The example's one schema migration is amended: the currency code is unique, a product's currency and category are foreign keys `ON DELETE RESTRICT`, the username is folded in Go into a `username_normalized` column with its unique key (in place of `LOWER(username)`), and it adds an identifier sequence and a per-account session index.

**Symptom.** A database volume built by an earlier checkout is refused by the migration's fingerprint at the first use that goes through the example's migration funnel — the first resolution of a repository, or the two-factor store's first request — not at boot: a process serving only `/health` does not notice it, and a `db:migrate` with no pending migration answers success without reading the fingerprint.

**Remedy.** The example's data is demonstration data: rebuild it with `example:db:reset --force`, which drops it and recreates the schema. Data worth keeping is backed up first and carried over by a migration of your own.

### Example: the catalogue refuses a taken currency code and a dangling reference

**What changed.** A currency's code is its identity: a second currency under a taken code, a rename onto one and the delete of a currency a product is priced in are refused with 409, and a product write naming a missing category or currency is refused with 400, where they stored a wrong `?currency=` conversion or answered 500. `CategoryService.DeleteById` refuses a category a product sits in with `ErrCategoryInUse`.

**Symptom.** Those writes answer 409 or 400 instead of succeeding or failing with 500.

**Remedy.** A client treats the 409 as a conflict it resolves — another code, or the products moved off the currency or the category first — and the 400 as a request to correct, not to retry.

### Example: the sign-in door reads a json body alone

**What changed.** The sign-in door reads an `application/json` body and answers any other content type with 415, before the credentials are read and without a login failure: a urlencoded form, which a page on another site can post, needed an anti-forgery token the door never had.

**Symptom.** A client posting the form encoding gets 415.

**Remedy.** Send `Content-Type: application/json` with `{"username": …, "password": …}`.

### Example: outside development the boot refuses the committed credentials

**What changed.** The example's credentials are configuration — `APP_JWT_SECRET`, `APP_INTERNAL_AUTH_SECRET` beside `APP_INTERNAL_AUTH_KEY_ID` and `APP_INTERNAL_AUTH_APP`, and `APP_ENCRYPT_KEYS` — with the development values committed so a checkout boots. With `MELODY_ENV` other than `dev`, a blank or committed value refuses the boot by name. An `APP_ENCRYPT_KEYS` entry without its `id:` is named by its position.

**Symptom.** A deployment that kept the committed values, or left one blank, no longer boots outside development.

**Remedy.** Set each of them to a value of your own before deploying, `APP_ENCRYPT_KEYS` as `id:key` entries; the example's README, "Before production", lists them.

### Example: the development accounts are seeded in development only

**What changed.** The accounts `user`, `editor` and `admin`, whose passwords are their names, are seeded into an empty directory in development only.

**Symptom.** Outside development a fresh directory holds no account, and nobody can sign in.

**Remedy.** Configure the catalogue database (`MYSQL_*`), which holds the account directory, then create the first account with `example:user:create <username> --role ROLE_ADMIN`, the password on standard input; the command refuses a password given as an argument, and refuses to run without the catalogue database, since a directory held in memory belongs to one process.

### Example: enrolling a second factor again needs the current one

**What changed.** Enrolling a second factor again replaces it only for a request presenting a current factor of the enrollment it replaces — a TOTP code on `X-2FA-Code` or a recovery code on `X-2FA-Recovery-Code`, spent as the verification door spends them; on a session alone it answers 401 with the challenge, where a stolen session became a durable second factor of the thief's.

**Symptom.** A re-enrollment carrying only the session cookie is answered 401.

**Remedy.** Send the current TOTP code or a recovery code with the re-enrollment. An account that lost both is reset by an operator with `example:twofactor:reset`.

### Example: an account holds at most five live sessions

**What changed.** An account holds at most five live sessions: a sign-in past the cap ends the oldest, at the sign-in door and at the firewall's login handler, through the session index the schema migration holds. A sign-out frees its place, and a session that ended elsewhere stops counting at the next sign-in.

**Symptom.** A sixth sign-in signs out the account's oldest live session, on whatever client holds it.

**Remedy.** A client handles being signed out by signing in again; one that opens sessions it does not use signs out of them. Without a database the index restarts from zero with the process.

### Example: event streams are capped per account and per process

**What changed.** An account holds at most four event streams open at once and the process 256, counted together across `/events/stream/` and the `/ws` websocket route; a stream past either cap is refused with 429 before anything is committed, and a closed stream frees its place.

**Symptom.** A fifth stream of one account, or any stream past the process cap, is answered 429.

**Remedy.** Close abandoned streams, share one stream per client where you can, and reconnect after a 429 with a delay rather than at once.

### Http: `NewRequirements` takes a slice of the pointers the helpers return, and refuses an incomplete or duplicated declaration

**What changed.** `http.NewRequirements` takes a `[]*Requirement`, the pointers `NewRequirement` and the `Require*` helpers return, nil declaring none, where the third major kept the released variadic `...Requirement` signature and a helper's result was passed dereferenced. It refuses four declarations, three of which it used to drop in silence: a nil requirement, an empty parameter name, an empty pattern, and one parameter named twice.

**Symptom.** A variadic call, `NewRequirements(*http.RequireNumeric("id"))` or `NewRequirements(http.RequireNumeric("id"))`, no longer compiles. A boot that used to succeed now panics by name if any declared requirement is nil, incomplete or duplicated.

**Remedy.** Drop the dereference and pass a slice: `NewRequirements([]*http.Requirement{http.RequireNumeric("id")})`. If the boot refuses, the refusal names the parameter and the index — an empty pattern is almost always a constant that was never filled in or a configured pattern that resolved to `""`, and until now it left that route parameter with **no constraint at all**, so a segment declared numeric matched anything.

### Http: `JsonHandler` refuses through the door `BindJson` uses, and `JsonHandlerErrorResponder` receives the failure

**What changed.** `JsonHandler` binds through the same door `Request.BindJson` uses, so its refusals now carry the decoder's diagnosis as a cause, answer 413 for an oversized body instead of 400, and serve validation detail under the `validationErrors` key. `JsonHandlerErrorResponder`, set as `JsonHandlerConfig.ErrorResponder`, is handed the failure itself as a fifth parameter beside the status and the message; the third major kept the released four-parameter form under that name and carried this one as `JsonHandlerFailureResponder`, set through `WithJsonHandlerFailureResponder`, both removed here. A nil responder leaves the refusals to the framework.

**Symptom.** Clients of a `JsonHandler` route see a 413 where they saw a 400 for an oversized upload, and the error body for a validation failure changes: `error` becomes `"validation failed"` and the per-field detail arrives in the `validationErrors` array, exactly as it already did for `BindJsonAndValidate` routes. A responder returning `(nil, nil)` no longer suppresses the refusal: previously that answered the client an empty **204** for a rejected request. A four-parameter responder, and a call to `WithJsonHandlerFailureResponder`, no longer compile.

**Remedy.** A client parsing the flattened message reads `validationErrors` instead; that array carries `field`, `message` and `code` per entry rather than a joined sentence. A four-parameter responder gains the `cause error` parameter, which it may ignore; a failure responder is set as `http.JsonHandlerConfig{ErrorResponder: responder}` and its type is named `JsonHandlerErrorResponder`.

### Http: a route pattern, a route zone and a trusted-proxy entry are refused when they are wrong

**What changed.** Four boot-time refusals replace four silent drops. A pattern naming one parameter twice (`/orgs/:id/members/:id`) or carrying a nameless `:` is refused at registration. A zone that is not one of the `RouteZone*` values is refused, at `ExposedRouteAttributes` and at registration, as are an exposed route with no name, an expose attribute that is not a bool and a zone attribute that is not a string. A trusted-proxy entry that parses as neither a CIDR prefix nor an address is refused by `NewKernel`, over `KernelConfig.ForwardedHeadersPolicy`, and by `NewForwardedClientIpResolver`.

**Symptom.** A boot that used to succeed now panics, naming the pattern, the zone or the offending entry.

**Remedy.** Each refusal names what to fix. The duplicate parameter was unreadable by the handler anyway — only one of the two values survived extraction, and which one was not the handler's to choose. The misspelled zone produced an artifact no filter could select: `melody:routes:manifest --zone <typo>` wrote an empty manifest over the good one and exited zero. The malformed proxy entry narrowed the trusted list, so `X-Forwarded-For` from that hop stopped being believed and every client behind it shared one rate-limit bucket. An unnamed catch-all (`*...`) is untouched, and an empty trusted-proxy entry is still skipped so a list split from an environment variable keeps working.

### Http: the route manifest carries the host, schemes, locales and priority

**What changed.** `RouteManifestEntry` gains `host`, `schemes`, `locales` and `priority`, all `omitempty`. Requirements are now published as the caller DECLARED them rather than as the anchored, non-capturing form the router compiles. Entries with equal names are ordered by pattern, so the document is stable. `FilterRouteManifestByZone` is exported so an in-process projection can apply the zone gate the cli command applies.

**Symptom.** A consumer that parsed the manifest sees new fields and a changed `requirements` spelling: `en|de` where it previously read `^(?:en|de)$`.

**Remedy.** A consumer applying the requirement as a regular expression should keep anchoring it itself; the published form is the developer's declaration, which is what a browser regular-expression engine can actually compile — the wrapped form leaked RE2-only syntax into ECMA-262 contexts. The three new fields are what a generated url needs to match on the way back in: a route bound to a host, restricted to a scheme, or restricted to a locale set was advertised with nothing saying so, and the url the consumer built was refused by the router that advertised it.

### Http: the server-sent-event writer refuses what it used to emit, and the hub closes what it owns

**What changed.** `NewServerSentEventWriter` refuses an already-committed response and a connection that cannot really flush (the probe now reads through the kernel's response-writer wrapper instead of at it). `Send` refuses an event carrying a name and no data, an id or name that would collapse to empty once its control bytes are removed, and a negative retry; a frame that failed partway poisons the writer, so every later frame is refused rather than appended onto torn bytes. The writer serializes its own frames, so a handler and a keepalive ticker may share it. `ServerSentEventHub` closes the backplane it owns on `CloseWithContext`, the door the container's teardown sees, refuses to install a different one over a live one or any into a hub that has already closed, and takes a logger through `ServerSentEventHubConfig.Logger`.

**Symptom.** A `Send` call that used to return nil may now return an error — most often `ServerSentEvent{Event: "x"}` with no `Data`, which the event-stream grammar discards without dispatching anything, so the listener that named `x` never fired. A composition root that installed a second backplane over a live hub now gets a refusal naming the situation, where the first one used to be orphaned in silence. An application that resolved the hub from the container will now have it closed by the ordered teardown.

**Remedy.** Clear the hub and close what you took out before installing a replacement. Give every named event a `Data` payload, or send a comment instead. Handle the error `Send` returns — a stream whose write failed is finished, and the writer says so. If a hub was being kept alive past the container teardown deliberately, do not register it as a service.

### Http: a request path that folds to a different spelling is refused with a 400 before the handler

**What changed.** The kernel refuses any request path that `path.Clean` would fold to another spelling — `//admin/panel`, `/open/../admin/panel`, `/a/./b` — before the security dispatch and before the handler, with a 400 and a warning naming the method and path. A trailing slash is not a fold and is served as before; a target that does not begin with `/` (the asterisk-form of OPTIONS, an authority-form CONNECT) is left to the router.

**Symptom.** A client that spelled a working url with a doubled slash or a dot segment — some crawlers, hand-built paths concatenated with a stray `/` — starts receiving 400 where the route used to answer. Every such request was previously routed under its sent spelling but authorized against the folded spelling's rule, which is the authorization bypass the refusal closes.

**Remedy.** Fix the client to emit the canonical spelling; the refusal is deliberately not a redirect, so nothing teaches the client the working spelling while sidestepping a firewall rule. There is no opt-out.

### Http: a request path padded with whitespace is refused with a 400 before the handler

**What changed.** The kernel refuses, with `400`, a request path that leading or trailing whitespace would be trimmed from — the decoded form of `/public%20`, `/public%09` or `/public%C2%A0` — after the route is matched and before it is authorized or handled, the way it refuses a path carrying `..`, `.` or `//`. The router keeps the whitespace, so `/public%20` reached a catch-all handler as its own spelling, while the access-control matcher trims it and authorized the request under the rule of `/public`: an exact `PUBLIC_ACCESS` rule beside a protected catch-all handler served the protected handler to an anonymous client. Whitespace inside the path (`/a%20b`) is read alike by every consumer and still routes.

**Symptom.** A client that sends a path ending, or beginning after a stripped prefix, in an encoded space, tab or no-break space is answered `400 bad request` where the request was previously routed to a handler, or answered `404`.

**Remedy.** Send the path without the padding; nothing legitimate names a resource by a trailing space. There is no opt-out: the previous behaviour let a request reach a handler under an authorization decision made for a different path, which is the defect the refusal closes.

### Http: a urlencoded body that failed to read or parse is refused, and a session save outage replaces the response

**What changed.** A `application/x-www-form-urlencoded` body whose read failed answers 413 when the size limit stopped it and 400 when the client broke it, before the handler — it used to be a warning beside an empty form, with the handler running and often answering 200. A multipart upload past `MaxRequestBodyBytes` surfacing from `ParseMultipartForm` is answered 413 instead of a 500 at error level. Separately, a session-storage outage on the save path replaces the handler's response with an empty `503` carrying `Retry-After` (see "Session: every operation takes the context first") and suppresses the session cookie; the handler's success used to be delivered with the session write silently lost.

**Symptom.** Endpoints that previously "accepted" oversized or truncated form submissions as empty forms now refuse them; a login or counter endpoint in front of a down session backend answers 503 instead of a success whose session never landed.

**Remedy.** None for the refusals — they surface failures that were being mis-answered. A handler that must serve degraded responses during a session-backend outage stops writing to the session on that path.

### Http: repeated request parameters are typed, and reading one as a single string answers its first value

**What changed.** A request parameter that appeared once is stored in the bags as a string; a genuinely repeated key (`?tag=a&tag=b`) stays a `[]string`, and `bag.String`/`bag.StringOrDefault`/`bag.HasNonEmptyString` on it answer its first value, as `Request.Input` and `url.Values.Get` do; the whole list is read with `StringSlice`/`StringAt`.

**Symptom.** Handlers that read single-occurrence query or post parameters through `Input`, `bag.String` or `StringOrDefault` start seeing the real values — they used to receive the empty string for every present parameter. Code that read a repeated key through `bag.String` receives its first value, where it used to receive `("", true)`.

**Remedy.** Read repeated keys whole with `bag.StringSlice` or `bag.StringAt`; nothing else changes for well-typed readers.

### Http: the method policy is `KernelConfig.MethodPolicy`, and duplicate routes are refused at registration

**What changed.** `http/contract.MethodPolicy` decides whether HEAD falls back to the GET route and whether an unrouted OPTIONS is answered; the framework kernel takes it from `KernelConfig.MethodPolicy`, `ApplicationConfig.HttpKernel.MethodPolicy` in an application; the optional `MethodPolicySetter` the third major asserted is removed. The route registry refuses a second route identical on everything the matcher discriminates (pattern, methods, host, schemes, locales, requirements, priority), because the later one could never be dispatched and was silently shadowed.

**Symptom.** A boot that registered the same route twice — usually a module wired twice, or a copy-pasted registration — now panics naming the pattern, the methods and the route name.

**Remedy.** Remove the duplicate registration; a route that must coexist differs in at least one discriminator. The application aggregates the boot collisions of its own registrations itself. Set the method policy in `ApplicationConfig.HttpKernel.MethodPolicy`.

### Http: every framework error goes through one renderer and answers the error envelope, and the validation detail is under `validationErrors`

**What changed.** Every error the framework renders for a request — the exception listener and the kernel's own fallback paths, the `404` and `405` among them — goes through one renderer, and its json body is the error envelope: `status`, `time` and, where the request is known, `requestId`, beside an `error` object carrying the `message`, the client-facing `code`, `key` and `parameters` of an exception that declares them, and in debug mode the `context` and the `cause`. `http.ErrorResponsePayload` carries `Status`, `Time` and `Error`, an `http.ErrorResponsePayloadDetail`, and `NewErrorResponsePayload` takes the status before the message and the time. `JsonErrorResponse`, which the security entry points and the examples answer through, takes no request and writes the same envelope unnegotiated, the request id travelling on the `X-Request-Id` header. The third major kept the released `{"error": "<message>", "time": ...}` body, the debug `context` and `cause` at its top level.

The body is also negotiated: a client that prefers html gets the html error page with the status and the request id, a `text/plain` client gets the body as lines, the error object's entries listed beside the request id and the time, anything else goes through the serializer manager against the joined Accept lines, and an Accept header that refuses every available type keeps the error's own status with the json body — an error status is the signal itself, and masking a refusal behind 406 would hide it. The per-field validation detail reaches the body under the top-level `validationErrors` key, projected so an entry blaming the rule DECLARATION keeps its field, message and code but loses the context naming the developer's own typo. v1/v2 spell that key `errors`; v3 and v4 diverge deliberately, because the key names what the detail is.

**Symptom.** A client that reads `error` as a string finds an object; a client that reads the debug `context` or `cause` at the top level finds them inside `error`; a body carries `status` where it did not. A call to `NewErrorResponsePayload(message, time)` and a read of `ErrorResponsePayload.Error` as a string no longer compile. A reader of the debug-only exception context keyed on `errors` finds `validationErrors`; a client that negotiated html, `text/plain` or another serializer's type for an error receives that type instead of json.

**Remedy.** Read the message at `error.message`, the debug detail at `error.context` and `error.cause`, and the validation detail at `validationErrors`; branch on `error.code` where the exception declares one. Read `payload.Error.Message`; the payload's constructor is unexported, see "Framework-only constructors left the surface".

### Exception: an http exception declares its client-facing code, key and parameters

**What changed.** `exception.NewHttpError(statusCode, exception.HttpErrorConfig{Code, Key, Message, Parameters, Cause, RetryAfter})` builds an `*exception.HttpException` that declares a client-facing identity: `code`, a stable value a client branches on, `key`, a translation key, and `parameters`, the values it is rendered with, read back through `Code`, `Key` and `Parameters`. The error renderer serves them inside the envelope's `error` object beside the message, outside debug mode too, each left out when empty; the parameters are copied at construction and on every read, and the framework translates nothing. An exception built through the other constructors declares no identity and its body is unchanged.

**Symptom.** None for an application that does not call it.

**Remedy.** An application that panics with a plain error carrying a status, a code and a translation key in its context, and installs its own kernel exception listener to re-derive the status and render them, can build the exception through `NewHttpError` instead and drop the listener: the framework listener renders the identity, and `AsHttpException` finds it through a wrapped chain.

### Http static: the folded root spellings are refused, the excluded index stays excluded, and embedded assets get real validators

**What changed.** The file server refuses the spellings `path.Clean` folds into the mount root — `/..`, `/.`, `//`, `/open/..` — the same refusal every other path already earned, because the matchers in front of the application compare the raw path and those spellings reach the index page from behind whatever rule the other prefix carries. The exclusion list is also consulted against the index file the root resolves to, so excluding `/index.html` excludes it for `/` too. On an embedded filesystem — where files carry no modification time — `Last-Modified` is no longer emitted as the year-one instant and `If-Modified-Since` is not consulted, the entity tag carries the build version instead of the zero timestamp, and the tag reads the modification time at nanosecond resolution. `NewFileServer` copies its configuration, proves an embedded public directory at construction, and honours a `CacheMaxAge` of zero as always-revalidate instead of coercing it to an hour.

**Symptom.** Requests for the folded spellings fall through to the application (usually a 404) instead of answering the index page; clients that conditionally revalidated embedded assets stop being answered 304 forever; a deployment that set `MELODY_STATIC_CACHE_MAX_AGE=0` with cache enabled starts serving `max-age=0`; a boot with `MELODY_PUBLIC_DIRECTORY` naming a directory the build did not embed panics instead of serving 404s.

**Remedy.** None for the refusals. A deployment that relied on the hour of freshness sets `MELODY_STATIC_CACHE_MAX_AGE=3600` explicitly; the server keeps its own copy of the `static.Config`, lists included, so a list changed after `NewFileServer` configures only the next server.

### Http cors: an empty origin list denies, entries are port-significant, and the preflight can be answered ahead of security

**What changed.** An explicitly empty (non-nil) `AllowOrigins` list denies every origin instead of silently becoming the wildcard, and credentials beside it are refused at boot under their own name. A schemeless allow entry is port-significant: `app.example.com` no longer matches every port of that host. `NewService` reads a nil method or header list as the default list — Authorization included — and an explicitly empty method or header list, which v3.13 replaced with its default, denies as the empty origin list does. `cors.RegisterRequestListener` and the `RegisterListeners` façade arrive, answering a preflight at priority 100, ahead of token resolution — a preflight carries neither cookie nor Authorization by spec, so aimed at an access-controlled path through the middleware form it was refused opaquely. `CorsMiddleware(nil)` read as the default service instead of panicking; the fourth major removes that family and refuses a nil service at `cors.Middleware` and the three listener doors, so the default service is spelled `cors.NewService(cors.Config{})`.

**Symptom.** A configuration whose origins variable arrives empty stops allowing everybody, and one whose methods or headers variable arrives empty stops granting any; an entry that relied on matching any port of its host stops matching the other ports; a SPA calling an access-controlled endpoint cross-origin starts working once the listeners are registered.

**Remedy.** Leave a list nil, not empty, where the default is meant; name the ports (or the scheme) in the allow entries; register `cors.RegisterListeners(eventDispatcher, service)` for applications with access-controlled cross-origin endpoints; keep the middleware form for applications without them.

### Http: the negotiation readers share one strict grammar

**What changed.** `PrefersHtml`, the compression middleware's `acceptsGzip` and the error-body negotiation read their headers under the serializer's rules: every line of a repeated field joined, members and parameters split outside quoted sections, the `q` parameter compared case-insensitively, and a member whose q falls outside the RFC 7231 qvalue grammar, zeros past the third decimal aside, dropped whole. A repeated Accept-Encoding coding resolves to its higher quality.

**Symptom.** `gzip;Q=0` stops being compressed; `q=Inf`, `q=NaN`, `q=5` and `q=-1` stop being honoured as weights and drop their member; a refusal carried in a quoted parameter (`text/html;p="a,b";q=0`) or on a second header line starts being honoured.

**Remedy.** None; clients emitting grammar-conforming headers see no change.

### Exception: an out-of-range status is refused at construction

**What changed.** `NewHttpException` and `NewHttpError` panic on a status outside `[400, 599]` (see "Exception: an http exception answers 400 to 599"). A 1xx is no final answer, so an exception raised with 103 was served as a 200 carrying the error body; net/http's `WriteHeader` panics on a status above 999 deep in the response path, and a status the writer clamps serves an exception as success.

**Symptom.** A constructor call with a mistyped status (a `4004`, a `0`) or an informational one (a `103`) panics where it is made instead of one response write away from it.

**Remedy.** Fix the status at the call site.

### Security: a global access control without a firewall now enforces, and a zero-value override inherits it

**What changed.** A `SetGlobal` access control declared without any firewall was silently dropped — `BuildAndCompile` returned nil the moment no firewall was registered — so the global rules enforced nothing. They now enforce: the compiled configuration carries the global control and the listeners fall back to it. Separately, a zero-value `FirewallOverride{}` inherits the global access control; the third major's bare override literal once carried no inheritance and compiled an empty control that opened every route behind the firewall.

**Symptom.** A deployment that declared global rules while every firewall sat behind a turned-off feature flag now has those rules apply — requests that used to reach handlers unauthenticated are refused. A firewall built with a bare `FirewallOverride{}` inherits and applies the global policy instead of leaving its routes open. `AddFirewall` refuses an unrecognised `FirewallOverride.MergeStrategy` by name where it used to fall back to `localFirst` silently.

**Remedy.** None if the enforcement is what you meant; this closes a fail-open. A firewall that genuinely wants no global inheritance sets `FirewallOverride.GlobalAccessControlDisabled` paired with `FirewallOverride.AccessControl`. Correct any merge strategy string the boot now rejects.

### Security: the role voters and `DecideAll` refuse where they used to grant

**What changed.** `RoleVoter` and `RoleHierarchyVoter` granted a token that carried the role even when it answered `IsAuthenticated()` false; they now deny it, matching the access control listener, which had always checked authentication first. `AccessDecisionManager.DecideAll` over an empty attribute list now refuses instead of granting vacuously, matching `DecideAny`. `SecurityContext.IsGranted` asks the same question before it reads roles, which is what makes the sentence below true: until it did, the voters denied while `IsGranted` still granted, so the firewall refused the route and a handler branching on `IsGranted` rendered the privileged content behind it.

**Symptom.** A handler-level `IsGranted` check on an unauthenticated token that carries roles — a "remembered" or half-logged-in token — now returns false where it used to return true. A direct `DecideAll(runtimeInstance, token, nil, subject)` caller now receives a refusal.

**Remedy.** None if you relied on authentication being required, which is the safe reading. A caller that intentionally passed an empty attribute list to `DecideAll` expecting a grant must pass the attributes it means to check.

### Security: the internal-auth envelope carries its own `typ`, and the JWT validator refuses it

**What changed.** The internal-auth envelope and the JSON web token were both HS256 over the same signing primitive in a byte-identical three-part shape, with nothing but secret disjointness — which nothing enforces — telling them apart. The envelope now signs under `typ: "melody-internal"`, the decoder REQUIRES that type, and the JWT validator refuses any `typ` that is not absent or `JWT` (case-insensitive). The third major's `HmacTokenSourceConfig.AcceptUntypedEnvelopes` opened a migration window in which an envelope carrying NO `typ` was accepted; the fourth major removes it, so an envelope without the type, or with a wrong one, is always refused.

**Symptom.** An envelope minted by an earlier build of the signer is rejected by an upgraded verifier ("internal-auth type is not accepted" in the journal at Info). With the default thirty-second envelope ttl, the exposure is one rolling-deploy window in a fleet where signers and verifiers upgrade at different moments. A JWT minted with an exotic `typ` (anything other than absent or `JWT`) is now refused.

**Remedy.** Roll every signer onto a build that stamps the type before a verifier moves to this major: a fleet that cannot do both within the envelope ttl of one another runs the third major's window, `AcceptUntypedEnvelopes: true` on its third-major verifiers, while the signers roll, and moves the verifiers here once none signs untyped. Otherwise upgrade both sides within the ttl or tolerate the one-window rejections; they fail closed to anonymous, exactly as an expired envelope does. A JWT issuer that stamps a non-standard `typ` must mint with `JWT` or drop the field.

### Security: both token stores refuse a lapsed ttl on `Put`

**What changed.** The third major's `InMemoryTokenStore.PutWithTtl` and `RedisTokenStore.PutWithTtl` stored the token with NO expiry when the ttl was zero or negative, and its v3.14.0 release made both panic on `ttl <= 0`. In the fourth major the one `Put` takes the ttl as a `bound.Duration` read against `securitycontract.TokenTtlRule()`, and both stores panic on an absent ttl and one under a second, naming the ttl; `bound.UnlimitedDuration()` is the explicit no expiry.

**Symptom.** A caller computing a token's remaining lifetime that has already elapsed, or is under a second, — the likeliest source of such a ttl — used to leave a permanent entry; it now fails loudly at the call site.

**Remedy.** A caller that genuinely means "no expiry" calls `Put(token, claims, bound.UnlimitedDuration())`. A caller passing a computed remainder hands `bound.Of(remainder)` only when it is at least a second, and treats a shorter or elapsed lifetime as the expired token it is.

### Security: the in-memory store keeps revocation boundaries until their retention passes

**What changed.** `InMemoryTokenStore.PurgeExpired` dropped every revocation boundary of a user who held no stored tokens; stateless JWTs are never stored, so a JWT-only deployment lost its boundaries on the first purge. In the fourth major the store keeps a boundary for `InMemoryTokenStoreConfig.RevocationEpochRetention`, 7 days when left out, the rueidis store's default, and `bound.UnlimitedDuration()` keeps it until the process exits, where the third major kept it forever by default. Separately, a negative `JwtConfig.RevocationEpochSkew` and a negative rueidis `TokenStoreConfig.MaximumClockSkew` or `RevocationEpochRetention` are refused at construction instead of narrowing a revocation or being silently ignored.

**Symptom.** A deployment minting stateless tokens that live longer than seven days loses a boundary while a token it refuses is still valid, unless a retention covering them is configured. A boot passing a negative skew or retention now fails at construction where it used to run a different policy in silence.

**Remedy.** Set `RevocationEpochRetention` to at least the longest token lifetime you mint — a boundary outliving every token it can refuse changes nothing thereafter — or `bound.UnlimitedDuration()` where token lifetimes are not bounded. Fix any negative duration the boot now rejects; the sign was never doing what its author meant.

### Bunorm: the registry refuses new callers while a pool is still closing

**What changed.** `ManagerRegistry.Close` marked the registry closed and then held the registry lock for the whole teardown, closing every manager and every migration database inside the critical section. It now publishes the flag under the lock, takes a snapshot of the two maps, releases the lock, and closes the pools outside it.

**Symptom.** A call to `Manager`, `Database` or `MigrationDatabase` arriving while `Close` is running no longer waits for the teardown to finish; it is refused at once with `ErrManagerRegistryClosed`. Previously such a call parked on the registry lock, and against a peer that had stopped answering — a network partition at shutdown, where the migration connection's write deadlines are deliberately lifted — it could park for as long as the driver waited, so a graceful-shutdown drain expired with goroutines wedged in the registry. Code that relied on that blocking to serialise its last queries behind the teardown now sees the refusal instead.

**Remedy.** None for the ordinary case: the refusal is what the flag has always meant, and every caller already had to handle `ErrManagerRegistryClosed`, which is what the same call answered a moment later anyway. A caller that genuinely needs its work to finish before the pools close must order that itself — run it before `Close`, or gate `Close` behind it — rather than relying on the lock to do the ordering.

### Bunorm mysql and pgsql: a transient marker inside a word is no longer transient

**What changed.** The providers decide whether to retry a failed open by scanning the lowercased error message for a list of markers. The scan matched them as bare substrings, so the short spellings fired inside ordinary identifiers. The markers are now matched as words: a match counts only where the characters on either side are not letters, digits or underscores.

**Symptom.** A permanent failure whose message happens to contain a marker inside a word now fails on the first attempt instead of being retried for the whole budget. The two measured cases are a missing table whose name contains `eof` — `Table 'app.geofences' doesn't exist` — and an unknown column named `session_timeout`; both were retried to exhaustion and then reported as "database connection failed after max retry attempts" rather than as a non-transient failure. Such a boot now fails faster and under the correct classification.

**Remedy.** None is required, and the change is in the safe direction: the failure was permanent in both cases and the retries only delayed the report. The `io.EOF` and `net.Error` checks that run ahead of the message scan are untouched, so a genuine end-of-file or timeout is classified by type as before, and every marker that appears as its own word — `i/o timeout`, `connection refused`, `bad connection`, a bare `EOF` — matches exactly as it did. An operator who wants a permanent failure retried anyway raises the retry budget rather than relying on a substring collision.

### Bunorm: `ReadWriteSplitter.Reader` falls back to the primary on an unreachable replica only

**What changed.** [`ReadWriteSplitter.Reader`](../../integrations/bunorm/v4/split.go) serves the primary in place of a replica only when the replica's provider filed its open failure under [`ErrDatabaseUnreachable`](../../integrations/bunorm/v4/errors.go) — an outage the transient classifier admitted and the retry budget could not get past. Every other failure of the replica is handed back. It used to refuse a denylist of four registry sentinels and absorb everything else.

**Symptom.** A replica whose user was left unset, whose password the server refused or which points at a database that does not exist used to be served from the primary on every read, silently and for the life of the process; the read now fails with the replica's own refusal. An unreachable replica — a host that is down, a closed port, the retry budget spent — still falls back to the primary as before.

**Remedy.** Repair the replica's configuration; the refusal names what the server refused. A provider of your own takes part in the fallback by filing an outage through [`DatabaseUnreachable`](../../integrations/bunorm/v4/errors.go) under its exception's cause — a provider that files nothing has every failure read as a refusal, the direction that surfaces a misconfiguration.

### Bunorm mysql and pgsql: a refusal the server gave by name is never an outage

**What changed.** The transient classifier of the [`mysql`](../../integrations/bunorm/mysql/v4/provider.go) and [`pgsql`](../../integrations/bunorm/pgsql/v4/provider.go) providers reads the identity a server refusal carries — the SQLSTATE on PostgreSQL, the error number on MySQL — before it scans the message for markers, and the caller's own context before it classifies at all. A refusal with an identity is transient only for the classes that mean the server cannot take the connection now (PostgreSQL `08`, `53`, `57`; MySQL 1040, 1053, 1159, 1203, 1226, and ProxySQL's 9001 and 9002 for the backend it fronts) and terminal for every other; a context the caller had already cancelled or let expire is the caller's stop, whichever class its refusal wears.

**Symptom.** A database named `timeout` or a user named `eof` — any operand the server quotes into its message that happens to carry a transient marker — used to be retried for the whole budget, reported as "failed after max retry attempts", and, behind a read/write splitter, served from the primary in silence; such a refusal now fails on the first attempt under its own name. An outage whose message carried no marker — a `57P03` "cannot connect now", MySQL's 1203 — used to be terminal and is now retried. A caller's expired deadline used to be filed as an unreachable database on the retry-less door and, on the retrying one, cost one retry and two warnings; it is now one warning and no attempt.

**Remedy.** None is required. An operator who wants a permanent refusal retried anyway cannot get that by naming the database after a marker; the retry budget applies to outages only.

### Bunorm mysql and pgsql: an empty host is refused

**What changed.** The six providers of the [`mysql`](../../integrations/bunorm/mysql/v4/provider.go) and [`pgsql`](../../integrations/bunorm/pgsql/v4/provider.go) families refuse an empty host by name before the driver configuration is built, beside the empty database and user the pgsql provider already refused.

**Symptom.** A configuration whose host was left unset used to connect: the dial address the drivers made from an empty host, `:port`, is the local system, so the application connected to whatever listened on that port on its own machine — with the configured credentials, the dialect handshake included — instead of failing. The open now fails, naming the empty host.

**Remedy.** Set the host — `MYSQL_HOST`, `PGSQL_HOST` in the example's spelling — on every environment that boots the application. A deployment that relied on the local dial by accident names `127.0.0.1` explicitly.

### Bunorm mysql and pgsql: a left-out pool or timeout field takes the default, and unlimited lifts a bound

**What changed.** Every left-out field of `PoolConfig` and `TimeoutConfig` in the [`mysql`](../../integrations/bunorm/mysql/v4/provider.go) and [`pgsql`](../../integrations/bunorm/pgsql/v4/provider.go) providers takes the default, and `bound.UnlimitedCount()` / `bound.UnlimitedDuration()` lift the bound where the field admits it: no deadline for a read or write timeout, no cap on the open or idle connections, no recycling for a lifetime or an idle time; the connect timeout is never lifted. A zero used to reach `database/sql` and the driver as it was, where it means an unlimited pool, connections never recycled and, on the mysql driver, no read or write deadline; and a negative connect timeout failed every mysql dial at once.

**Symptom.** A configuration that set a field to zero to mean "no limit", as the released changelogs of mysql v3.1.0 and pgsql v3.1.1 described for `ConnectTimeout`, now runs under the default: a connect timeout of 10 seconds on mysql and 5 on pgsql, a pool of the default size, connections recycled on the default lifetime. The same happens to a zero that came from an environment key nobody set, which is the case the change is for.

**Remedy.** Set the field to unlimited (the text `unlimited` read from configuration) where no bound is wanted, and leave the rest out for the defaults. The migration connection keeps its lifted read and write deadlines and its disabled recycling whatever the configuration says.

### Bunorm mysql: the provider negotiates verified TLS by default

**What changed.** The mysql provider set no TLS on its connector, so it connected in plaintext and offered no option to enable TLS. It now builds a verifying `tls.Config` by default — the system roots, the configured host as the name to verify against, `MinVersion` TLS 1.2 — the same posture its pgsql sibling already carried, and refuses the driver spellings that would downgrade silently.

**Symptom.** A mysql server that speaks no TLS fails the dial where it previously connected in plaintext. The example's development mysql is such a server.

**Remedy.** A database reached over a trusted network, or one that speaks no TLS, sets `Insecure: true` on the provider's `ProviderConfig` — the deliberate opt-out spelled the same way as pgsql's. A database with a certificate needs no change; one needing a pinned or client certificate sets `TlsConfig`. The example arms the opt-out through a new `MYSQL_INSECURE` switch in its `.env`.

### Bunorm: bun's own diagnostics go to the journal

**What changed.** Opening a connection through the mysql or pgsql provider routes bun's package-level logger into the application's journal, once per process, through the new `bunorm.RouteDiagnostics`. Bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule, a query carrying arguments and no placeholders — arrive as warning records under the message `bun diagnostic` with the line in the context.

**Symptom.** Those lines stop appearing on standard error and start appearing in the journal. An operator or a test grepping standard error for `WARN: bun:` finds nothing.

**Remedy.** Read them from the journal, filtering on the `bun diagnostic` message. One line is deliberately unaffected and stays on standard error: the mysql dialect writes `can't discover MySQL version` through the **standard library's** default logger rather than bun's, so routing it would mean taking `log.SetOutput` for the whole process — every dependency and your own `log` calls with it. That is the application's decision; take it in your composition root if you want it, as the mysql readme shows.

### Bunorm pgsql: every driver deadline is named, configured and lifted for migrations

**What changed.** `pgsql.TimeoutConfig` carries `ReadTimeout` and `WriteTimeout` beside `ConnectTimeout`, the connector receives all three (the dial included), and the provider implements `bunorm.MigrationProvider`. Until now the dial ran under pgdriver's internal 5s default whatever `ConnectTimeout` said, every query ran under invisible 10s read / 5s write deadlines, and `db:migrate` ran on the request pool — an 11-second DDL statement died at 10.004s, measured.

**Symptom.** The effective read/write deadlines move from 10s/5s to the documented 30s/30s. `NewTimeoutConfig` and `NewTimeoutConfigWithDeadlines`, the name the three-duration form carried on the third major, are gone, for the `TimeoutConfig` literal.

**Remedy.** `TimeoutConfig{ConnectTimeout: bound.Of(connect)}` keeps the connect timeout and takes the 30s/30s defaults; name tighter deadlines through `ReadTimeout` and `WriteTimeout` where request traffic needs them, `bound.UnlimitedDuration()` lifting one. Migrations need nothing: `db:migrate` now prefers the dedicated lifted connection automatically.

### Bunorm: the `bun` requirement moves to v1.2.17, dialects and drivers in lockstep

**What changed.** Every module of the `bunorm` family — the manager, `mysql`, `pgsql` and the three `migrate` modules — requires `github.com/uptrace/bun v1.2.17` and, where they carry one, `dialect/mysqldialect`, `dialect/pgdialect` or `driver/pgdriver` at the same version. v1.2.16 swallowed the failure of a migration read from a `.sql` file: the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec error with its own nil return, so `db:migrate` printed `[success]`, exited 0 and marked a migration applied that never ran.

**Symptom.** If your application pins a bun dialect or driver of its own, the build now selects `bun v1.2.17` through this dependency while your dialect stays where it was, and the process **panics at init**: `mysqldialect and Bun must have the same version: v1.2.16 != v1.2.17`. The dialect packages check this themselves; it is not a melody rule.

**Remedy.** Move your own `github.com/uptrace/bun/...` requirements to `v1.2.17` in the same change — `go get github.com/uptrace/bun@v1.2.17 github.com/uptrace/bun/dialect/mysqldialect@v1.2.17` and the equivalent for `pgdialect` / `pgdriver`. Applications that declare no bun dependency of their own need no action.

### Bunorm migrate: the held-lock refusal names the resource and the remedy

**What changed.** `<prefix>:migrate` and `<prefix>:rollback` wrap bun's lock error in a melody error naming the manager label and, but on a cancelled context, the lock table — and, when the lock is held by another run, the `<prefix>:unlock` command —, with bun's error kept as the cause; the wrap is one of four, for a cancelled context, a locks table missing or unreachable, a held lock and a lock that could not be taken. It previously travelled as bun's own error, carrying no melody context at all.

**Symptom.** Code matching the refusal on the text `already locked` no longer matches at the top of the chain: `Error()` answers `migrate: the migration lock is held; another migration is running, or a crashed one left it behind`.

**Remedy.** Match through the chain — `errors.Is` and `errors.As` reach bun's error exactly as before — or read the `manager` key of the context, `locksTable` on every refusal but the cancelled context's, and `unlockCommand` on the held-lock refusal, which is what the wrap exists to provide.

### Bunorm migrate: a failed lock release fails the command

**What changed.** A `db:migrate` or `db:rollback` whose unlock fails now returns that failure instead of printing it and exiting 0. A migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it. The release also runs on a context detached from the command's own, so an interrupted migration no longer leaves the lock row behind.

**Symptom.** A deploy step that read exit 0 over a surviving lock row — and then found every later migration refused on every replica — now fails at the step that caused it.

**Remedy.** None for a healthy run. For the failure, `<prefix>:unlock` clears a lock a crashed process left behind, and the refusal above names it.

### Bunorm migrate: the json document is not shaped by `--verbose`, and its keys are stable

**What changed.** Under `--format=json`, `db:migrate`, `db:rollback`, `db:status`, `db:init` and `db:unlock` render one machine-readable envelope where they used to print the plain-text blocks, and they collect every block at any verbosity: verbosity remains a rendering decision about the plain text alone, which is what the readme always said. The document keys are stable names rather than display headings — `data.migrations.applied`, `.pending`, `.rolledBack` — and `data.database.database` is json `null` when the connection reports no current database, where the text block renders `<null>`. `--format=json-pretty` is the same document indented for reading by hand.

**Symptom.** `db:status --format=json | jq` used to fail on the first byte of a plain-text table; it now decodes. A json run performs the database-identity query that a text run performs only under `--verbose`.

**Remedy.** Read `data.migrations.applied`, `data.migrations.pending` and `data.migrations.rolledBack`; test `data.database.database` for null rather than for the string `"<null>"`. Anything that parsed the plain text under `--format=json` must decode the document instead — which is what the flag always promised.

### Bunorm migrate: a nil migration set and an empty module configuration are refused

**What changed.** `RegisterCommands(nil, ...)` panics at registration instead of answering no commands, and `NewModule(ModuleConfig{})` — neither `Migrations` nor `Contexts` — is refused by name when the kernel asks it for its commands. Both used to be silent.

**Symptom.** A binary whose wiring passed a nil set, or registered the module with an empty configuration, fails at boot with a named refusal where it used to boot and answer `unknown command` at the first `db:migrate`.

**Remedy.** Pass the migration set, or the contexts, that the registration was meant to carry. A binary that registers only `Contexts` is unaffected: the module gates its own optional set before calling `RegisterCommands`.

### Bunorm migrate: the plain text escapes control characters, and the commands stop pre-printing their failure

**What changed.** Every string the commands did not write themselves — the error text off the wire, the failed statement, the query names, the identity block the server answers and the migration names — is escaped visibly (the named C0 characters as `\n`, `\r`, `\t`, every other C0 one, DEL and the C1 block as `\xNN`, and the Unicode line and paragraph separators as `\uNNNN`) before the terminal sees it, and before the table cells are measured, so the alignment counts the escaped spelling. The failed statement alone keeps its real line breaks. Separately, the commands no longer pre-print the failure they return: the cli runner's `[error]` line and the log record already report it. The json rendering is the framework printer's, which spells the C1 block the encoder leaves raw as `\u00NN` escapes and decodes to the same values.

**Symptom.** A test asserting an exact rendered line that contained a raw control byte sees its escaped spelling. A console that showed the same failure three times shows it twice.

**Remedy.** Assert on the escaped spelling, which is what an operator's terminal now receives. The deliberate exception stays: an unlock failure beside a failed migration is still printed, because the return keeps the migration's error and would otherwise lose it.

### Bunorm migrate: the verbose DATABASE block answers for PostgreSQL, and cells are cut by runes

**What changed.** The `--verbose` DATABASE block was MySQL-only, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable; it is answered now through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. Table cells are truncated by rune count rather than by bytes, matching the format widths that pad by runes.

**Symptom.** `db:migrate --verbose` against postgres prints a DATABASE block where it printed none. Over a unix socket — the connection a local migration is most likely to take — the host reads `<local socket>`, because `inet_server_addr()` is NULL there. A multi-byte value that fit its column is no longer truncated, and no cut lands mid-rune.

**Remedy.** None. Read the block; it reports the same five fields on both dialects.

### Bunorm migrate: the output posture of a migration is the running command's

**What changed.** `db:migrate` and `db:rollback` hand their `--no-color` and `--format` posture to the migrations through the context, and `RunQueries` reads it there for an option that leaves it out; the fourth major keeps no process-wide fallback beside it, as the section on the runner option above describes.

**Symptom.** A migration that dropped its context printed under the running command's posture through the process-wide fallback; it now prints to standard output.

**Remedy.** Pass the migration's context on to `RunQueries`, as the generated skeleton does, or name the writer and the colour in the `RunnerOption` it takes.

### Rueidis: the boot ping is bounded even when its timeout is left at zero

**What changed.** `Provider.Open` runs its boot ping under the resolved boot ping timeout: an absent `ClientConfig.BootPingTimeout` takes the default (3s) rather than meaning "no bound", and in the fourth major a value under a millisecond or an unlimited one is refused at `NewProvider`. It previously built the ping context only for a positive value and ran the ping on `context.Background()` otherwise — with no deadline at all.

**Symptom.** A configuration naming only the command timeout, the ordinary shape of a partial literal, put the boot ping on an unbounded context. A store that accepted the connection and then stopped answering hung the boot forever, holding a client no one could close yet. That boot now fails after the default bound with `redis connection failed`.

**Remedy.** None for a wiring that meant the ping to be bounded — this is the bound it always documented. A wiring that genuinely wanted an unbounded ping must now say so by turning the ping off (`ClientConfig.BootPingDisabled: true`) and pinging itself; a value large enough to stand in for "unbounded" also works, and reads as the deadline it is.

### Rueidis cache: `Backend.CloseWithContext` ends the backend, and `ClearByPrefix` refuses the empty prefix

**What changed.** Two refusals the in-memory backend behind the same contract already gave. `Backend.CloseWithContext` marks the backend closed and every later operation answers `cache backend is closed`, through the backend `BackendFromRuntime` answers too. And `ClearByPrefix("")` is refused as an empty key instead of being read as the whole namespace. The shared `rueidis.Client` is still not closed by the backend: it belongs to whoever built it.

**Symptom.** Code that used a backend after closing it — a teardown-ordering bug — used to keep serving through the client and now fails immediately with a named refusal. Code that passed an empty prefix to `ClearByPrefix`, deliberately or because a prefix assembled at run time came out empty, used to wipe every entry under the backend's own prefix and now gets `cache key is empty`.

**Remedy.** Fix the teardown order so nothing reads through a closed backend, which is what the refusal is for. Where the whole namespace really is meant, call `Clear`, which says so. `BackendConfig.CallTimeout` covers the other half of the same problem: it bounds every round trip on top of the caller's context.

### Rueidis: the rate limiter returns its failure, keys a counter by its name, and `OnError` and `Logger` are removed

**What changed.** [`RateLimiter`](../../integrations/rueidis/v4/rate_limit.go) implements the one `RateLimiter` contract (see "Http: one `RateLimiter` contract answers a decision, and a `429` carries `Retry-After`"): `Allow(runtimeInstance, key)` and `Reset(runtimeInstance, key)` run on the runtime's context capped by `CallTimeout`, a cut naming `RateLimiterConfig.CallTimeout`, and return their failure, marked when the store could not answer and unrecorded, so the http middleware and the gate record it once and a caller of `Reset` records its own. `RateLimiterConfig.OnError` and `Logger` are removed: both existed because `Allow` answered a bool and `Reset` nothing. The script answers the counter's ttl beside the count, `RetryAfter` on a denial, a key without a ttl answering none and being re-armed. A counter is keyed `<KeyPrefix><name>:<key>`, so two limiters over one client, which shared a counter under the default prefix, count apart, and a limiter keeps its counters when its limit or window changes.

**Symptom.** `unknown field OnError in struct literal of type rueidis.RateLimiterConfig`; `limiter.Reset(key)` no longer compiles; a failed reset no longer reaches a journal by itself. Every counter restarts once at the upgrade, and a monitoring script matching `melody:rate_limit:<key>` finds nothing.

**Remedy.** Drop `OnError` and `Logger`; record the error `Reset` returns, `if resetErr := limiter.Reset(runtimeInstance, key); nil != resetErr { … }`; an application that counts failures wraps the limiter. Match `<prefix><name>:<key>`; nothing is needed for the counters, which start afresh.

### Rueidis cache: a negative ttl is refused, and a batch failure names one key deterministically

**What changed.** `Set` and `SetMultiple` refuse a negative ttl instead of falling into the branch that writes no expiry at all, and `SetMultiple` judges the ttl before its empty-batch early return. A batch that fails part-way collects every failing key and names the sorted first, with the failed and requested counts beside it, rather than returning whichever failure the map iteration reached first. `Clear` and `ClearByPrefix` scan every node of the client rather than the one connection a single command reaches.

**Symptom.** A negative ttl used to store the entry **forever** — the one value the caller meant to be short-lived was the one value stored without end. A batch that failed named a different key on every call for the same wrong batch, and hid that the entries after it failed too. Against a redis cluster, a wipe deleted one node's share of the matching keys and reported success.

**Remedy.** Zero still means no expiry, as both backends document, so only a genuinely negative duration is affected — usually a subtraction that went past the present. Anything that parsed the batch failure message should read `key`, `failedKeyCount` and `requestedCount` from the error context instead.

### Cron: a module with runner commands and no configuration refuses the boot

**What changed.** `cron.NewModule`'s `RegisterCliCommands` panics when runner commands, `ModuleConfig.Runner.Commands` in the fourth major, are supplied without a `Configuration` factory, and when the factory returns nil. Until now the module silently registered nothing and the wiring error surfaced as "unknown command" at invocation.

**Symptom.** A wiring that carried runner commands but never set the configuration now fails at boot naming the missing configuration.

**Remedy.** Set `Configuration` (or a factory that returns one); a parameters-only module — no runner commands, no generator — keeps working without one.

### Cron: the runner accepts a command answering the same flag instances, and refuses a nil flag at construction

**What changed.** `NewRunnerCommand` no longer refuses a command whose `Flags()` answers the same instances on every call: the cli dispatch hands the engine fresh instances of a command's flags, so such a command reads fresh parse state on every tick. `ErrSharedRunnerCommandFlags` is removed. A command answering a nil flag, typed or untyped, is refused at construction with `ErrNilRunnerCommandFlag`, where the typed nil was reported as shared flags and the untyped one was refused at the first tick.

**Symptom.** A memoizing command that refused the boot now runs; a command answering a nil flag refuses the boot instead of failing its first tick. A check on `ErrSharedRunnerCommandFlags` no longer compiles.

**Remedy.** Drop any workaround that rebuilt the flags on every `Flags()` call, if you added one only for the runner; match `ErrNilRunnerCommandFlag` where you matched `ErrSharedRunnerCommandFlags`, and drop the nil entry from what `Flags()` answers.

### Cron: an entry routed to another crontab file refuses the in-process runner

**What changed.** `EntryConfig.DestinationFile` joins `Command` and `Instances` in `NewRunnerCommand`'s construction refusal: an entry routed to another crontab addresses an external scheduler, and accepted by the runner as well it executed twice whenever the generated manifests were live.

**Symptom.** A boot that used to succeed panics with a message that opens with `cron: the in-process runner supports only name-scheduled single-instance entries; the entry routes to another crontab file`.

**Remedy.** Keep the routed entry out of the runner's `Configuration` (schedule it only for the generator), or drop its `DestinationFile` if in-process execution is the intent.

### Cron: a clean shutdown is not a job failure

**What changed.** A run the runner's own shutdown cancelled is recorded at warning as `cron: scheduled command cancelled by shutdown` and excluded from the failure aggregate; the runner's failure and abandon records carry the run's `cronRunId`.

**Symptom.** `melody:cron:run --once` interrupted by SIGTERM exits 0 with a warning instead of non-zero with an error record; alerting keyed on `cron runner command failed` stops firing on deploys.

**Remedy.** Key deploy-time alerting on the new warning if the old signal was load-bearing; genuine failures keep the error record, now attributable by `cronRunId`.

### Cron: `Configuration` hands out copies

**What changed.** `Schedule` copies the entry configuration it is handed, and `Entries` copies all the way down — the list, each `*ScheduledCommand` and each `*EntryConfig` behind it, schedule included.

**Symptom.** Code that reconfigured a registration by writing through its own struct after `Schedule(...)`, or through what `Entries` returned — `configuration.Entries()[0].Config.Schedule.Hour = "23"` — no longer changes anything.

**Remedy.** Register the entry with the configuration it should have. `Entries` is an inspector; the registry is written through `Schedule`.

### Cron: the runner writes the machine document, and a job's output goes to the journal

**What changed.** `melody:cron:run` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity` for every command, which used to kill the runner at parse — and under `--format=json` renders one envelope per dispatched minute (`--once` writes exactly one and exits). A scheduled command's own output no longer reaches the process stdout: it is captured and filed as one record per run that printed anything, naming the command and the run id, capped at 64 KiB.

**Symptom.** `melody:cron:run --once --format=json` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". Anything tailing the stdout of `melody:cron:run` for a job's own printed output finds it in the log instead, under `cron: scheduled command output`.

**Remedy.** Read the document rather than inferring the outcome from an empty stream. For a job's own output, read the journal; a job that must write to a stream of its own should open it itself rather than relying on the command writer.

### Cron: a recovered panic carries its cause

**What changed.** The cron runner's recovery boundary hands the panic value on as the CAUSE of the error it fabricates, and captures the stack of the goroutine that raised it. The `panicValue` context key it already wrote is unchanged; `panicStack` is added beside it.

**Symptom.** `errors.Is` and `errors.As` on the run's error now reach the failure underneath, where before they stopped at the fabricated wrapper. Code that relied on those calls answering false for a panicked job will now see them answer true.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked job the same way; the message still says the boundary was a panic.

### Cron: the ownership line names the application

**What changed.** The ownership line every generated destination opens with — the two crontab dialects in their header block, the `k8s` dialect as a leading YAML comment — reads `# owned by melody:cron:generate for <cli name>`, the name the application runs under after the command, instead of the bare `# owned by melody:cron:generate`. `melody:cron:generate --prune` matches the whole line, so it empties only the destinations this application wrote: the bare line was a package constant, identical in every melody binary, and two applications sharing an output directory had each other's crontabs emptied by the other's sweep. `CrontabOwnershipMarker` keeps its value as the line's prefix. A run whose configuration carries no application name writes the bare prefix and refuses to sweep; a name spanning lines is refused before anything is written. The name is `MELODY_CLI_NAME`, which the framework defaults to `melody`: two applications that both leave the default render one identical line and
still empty each other's destinations, so applications sharing a directory run under distinct names.

**Symptom.** Generated files gain the application's name on their ownership line; a byte-exact comparison against previously generated files sees the difference. A destination written by an earlier release carries the bare line and is no longer swept by `--prune`: an entry retired before the upgrade keeps running until its file is removed.

**Remedy.** Regenerate — every destination the current configuration names receives the line with the name and is swept by later runs as before. Remove once, by hand, the destinations a version before the upgrade retired. A custom dialect that two applications may share carries the application's name in its own ownership line from construction; the umbrella readme's example shows the shape. `--prune` sweeps only on a line that ends in ` for ` and this application's cli name, whichever template answered it: a dialect of yours that embeds a builtin and is registered under its name answers the bare prefix and is written but not swept — the run is refused at the sweep, naming the line.

### Cron: the generated k8s manifests open with the ownership marker

**What changed.** Every file the builtin `k8s` template renders starts with three comment lines carrying the ownership line — `# owned by melody:cron:generate for <cli name>`, the application's name after the command since the section above —, the same line the crontab dialects carry in their header block, and the template renders the marker header alone — demanding no container image — when it has no entries. That is what lets `--prune` reconcile a k8s output directory: the sweep empties only a file whose leading line, matched whole, proves this application's generator wrote it.

**Symptom.** Manifests gain three leading YAML comment lines; a byte-exact comparison against previously generated files sees the difference. `kubectl apply` reads comments as comments — the documents are unchanged.

**Remedy.** Regenerate; anything diffing manifests byte-for-byte should regenerate its baseline. Nothing to do for the cluster.

### Cron: a malformed heartbeat opt-in fails the generation

**What changed.** A `melody.cron.heartbeat.enabled` value that does not hold a boolean fails `melody:cron:generate` with an error naming the parameter, under every template. It used to be read as "not enabled".

**Symptom.** A generation that silently produced a crontab without the liveness line the operator asked for — a misspelling indistinguishable from never having asked — now exits non-zero naming `melody.cron.heartbeat.enabled`.

**Remedy.** Fix the value; `true`/`false` in any spelling the parameter conversion accepts. Removing the parameter keeps the opt-in unset, which stays legal.

### Cron: a relative parameter path anchors at the project directory

**What changed.** A relative path read from a parameter — `melody.cron.destination_file`, `melody.cron.logs_directory`, `melody.cron.heartbeat_path`, `melody.cron.binary` — is anchored at the project directory, the way melody resolves `kernel.logs_directory` and its siblings. A relative path typed as a cli flag stays relative to the working directory, as a shell path should.

**Symptom.** `melody.cron.logs_directory = var/log/cron` used to mean a different directory depending on where the binary was invoked from — under a supervisor starting from `/`, the generated crontab baked `/var/log/cron` into itself. The shipped defaults carry `%kernel.project_directory%` and never moved.

**Remedy.** None for parameters using the shipped `%kernel.project_directory%`-anchored defaults or absolute paths. A deployment that relied on a relative parameter path following the working directory should either make the path absolute or pass it as the command's flag, which stays relative to the working directory; running the generator from another directory no longer changes where a parameter path points.

### Cron: the generator writes the machine document and names what a failed run left on disk

**What changed.** `melody:cron:generate` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity`, which used to kill the generator at parse — and under `--format=json` renders one closed envelope: `data.writes` and `data.pruned` as lists on every run, the failure inside the envelope's error with its context and cause chain. In text mode a failed run prints the writes and sweeps that had already happened before returning the failure.

**Symptom.** `melody:cron:generate --format=json | jq` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". The heartbeat-only text line now says `wrote heartbeat-only crontab to …`, as the frozen majors' does.

**Remedy.** Read the document rather than inferring the outcome from the exit code alone; anything matching the old `heartbeat-only file` text updates the one word.

### CLI: `--format=json` writes one document per line

**What changed.** The json printer no longer indents. Every melody command's `--format=json` envelope — the framework's `debug:*` family and the core commands it contributes — is now one compact line terminated by a newline, where it used to be a block of indented lines. `--format=json-pretty` is the same document with the indentation back.

**Symptom.** Output that was read by eye, or a test asserting the rendered text with the spacing `encoding/json` puts after a colon (`"error": null`), sees the compact spelling instead (`"error":null`). Nothing that decodes the document is affected: it is the same document.

**Remedy.** For reading by hand, use `--format=json-pretty`, or pipe through `| jq`, which the documentation already recommended. For an assertion on rendered output, decode the document and assert the value rather than the text — the format the printer chooses is not part of what the command reports. Consumers that read the stream a document at a time, and every `jq` pipeline, need no change at all; the reason for the change is the consumers that could not work before, since a long-running command that renders a document per unit of work promised a stream of closed documents and emitted fragments.

### Cli: a duplicated flag name and a mismatched table row fail fast

**What changed.** `output.MergeFlags` panics on a flag name declared twice, and on a nil flag — the parser resolves a name to the first declaration, so a command-specific flag reusing a standard name was silently inert. `TableBlockBuilder.AddRow` panics on a row whose cell count disagrees with the block's declared columns — a surplus cell silently never rendered; the single-token separator row stays admitted.

**Symptom.** A command whose flags redeclare a standard name, or whose table rows disagree with their block's columns, panics at registration or at the row instead of silently misbehaving.

**Remedy.** Rename the colliding flag (the standard names are the `FlagName*` constants), or make the row's cell count match the columns.

### Cli: a flag spelling declared twice, an empty alias, a help spelling and a nil flag are refused at registration

**What changed.** `cli.Register`, `Root.Register` and every `cli.DispatchCommand` refuse, by name, a spelling a command's flags declare twice — a name or an alias, across all of its flags —, an empty alias, a flag spelled like the engine's own help flag (`help`, `h`) and a nil flag. The parser resolved a repeated spelling to the first flag declaring it in silence, and a command flag spelled `h` ran the command where `-h` should print its usage.

**Symptom.** The boot panics at the registration with `cli flag spelling declared twice` (the context names the spelling, the flag and the flag that declared it first), `cli flag alias is empty` or `cli flag may not be nil`; a command run through `DispatchCommand` fails the same way on every dispatch.

**Remedy.** Rename the colliding alias or flag, drop the empty alias, and drop the nil entry from what `Flags()` answers.

### Cli: negative values for the standard integer flags are refused

**What changed.** `--verbosity`, `--limit` and `--offset` carry validators refusing a negative value, and `--table-width` one refusing a value below 1, the way `--format` and `--order` refuse an unsupported one. A negative was clamped to zero, which read as unlimited for the limit — an argument asking for less than nothing silently delivered everything.

**Symptom.** `--limit=-5` fails at argument parsing, naming the flag, instead of listing everything with exit 0.

**Remedy.** Leave `--limit` out to list every item, `--limit=0` lists none; leave `--table-width` out for 120 or pass a positive width; `0` stays a value for `--offset` and `--verbosity`.

### Cli: the standard integer flags and the framework's own integer flags parse in base ten

**What changed.** `--verbosity`, `--limit`, `--offset` and `--table-width` of `output.StandardFlags()`, and the framework's own integer flags — `melody:messagebus:consume --limit` and `--concurrency`, the outbox relay's `--limit`, the bunorm encrypt command's `--batch` — declare `output.DecimalIntegerConfig`, so they parse in base ten. They used the engine's inferred base, which reads a leading `0` as octal and `0x` as hexadecimal.

**Symptom.** `--limit=010` reads ten where it read eight, `--limit=08` reads eight where it was refused, and `--limit=0x10` is refused at argument parsing where it read sixteen.

**Remedy.** Write the value in decimal. A command's own integer flag is not changed: it declares its base through its `Config`, `output.DecimalIntegerConfig` or another, and left unset keeps the engine's inferred base.

### Cli: a default its own validator refuses is refused at registration when the flag sets `ValidateDefaults`

**What changed.** `cli.Register`, `Root.Register` and every `cli.DispatchCommand` run the validator of a flag that sets `ValidateDefaults` over the value the engine builds from its declared default — the declared value itself for the string, int and bool flags, a copy of the declared slice, empty rather than nil when none is declared, for the string-slice flag — and refuse a refused default by name. The engine refused such a flag at every run of the command, without naming it. A flag that leaves `ValidateDefaults` false is not checked, as before: the engine validates only the values it parses.

**Symptom.** The boot panics at the registration with `cli flag default value is refused by the flag's own validator`, the context naming the flag and the refusal, where every run of the command failed; a command run through `DispatchCommand` fails the same way on every dispatch.

**Remedy.** Declare a default the validator accepts, or drop `ValidateDefaults` when the validator is meant for given values only.

### Cli: the table format stops hiding warnings and errors, and printing failures fail the command

**What changed.** Three output changes in the table format. The `WARNINGS` block renders under `--quiet` too — with `StandardFlags` defaulting quiet to true, an application command's warning was invisible at every verbosity; the warning details stay behind `--verbose`. The envelope error now renders whole (message, code, details, cause) — it previously rendered nowhere in the table format. And the first write failure is returned instead of discarded, so a report truncated by a full disk no longer ends in a success banner and exit zero.

**Symptom.** Quiet table runs may print new `WARNINGS:`/`ERROR:` lines; a run whose output stream fails now exits non-zero.

**Remedy.** None for correct runs; output parsers that assumed quiet suppressed warnings read the json format instead, which has always carried both.

### Cli: a failed run reaches the application log

**What changed.** The exit-coded errors built from a rendered envelope and from the command-suggestion refusal travel unmarked, so the exit path logs them through the application logger before the teardown. They were pre-marked as logged while the rendered report lived only on stdout/stderr — a failed run was invisible to anything reading the log file.

**Symptom.** The log file gains one record per failed command run and per mistyped command name. Exit codes are unchanged.

**Remedy.** None; log-volume alerts keyed on error records may need the new entries accounted for.

### Cli: the command action leaves the container to the process-exit owner

**What changed.** A registered command's action closes the request scope and reports its teardown failure beside the command's own error, and no longer closes the service container. The handler that owns the process exit closes it, after it resolved the logger the final record is written through.

**Symptom.** A container close failure is now reported by the exit handler rather than folded into the command's aggregate, and a failed command's final record is written through the live application logger instead of the stderr fallback. A command that closes the container itself is unaffected.

**Remedy.** None. A command that relied on the container being closed by the time its action returned should close what it owns itself, or use the scope.

### Application: a teardown failure on Run's normal return exits non-zero

**What changed.** When `Run` returns without a panic, the teardown it performs turns a failure it discovered itself — a container close that errored, a flush that failed — into exit code 1. Previously the failure was logged at emergency and the process exited 0.

**Symptom.** A supervisor that restarts on non-zero now sees a shutdown that lost something, where it used to record a clean stop whose only trace was one log line. A close somebody else already performed keeps reporting through its own channel and its own exit code, so nothing is reported twice.

**Remedy.** None for a healthy teardown — exit 0 is unchanged. A deployment that alerted on the emergency record alone can keep doing so; the exit code is an additional signal, not a replacement.

### Application: a teardown that hangs is abandoned and exits non-zero

**What changed.** The normal return of `Run` closes the container through the shield the panic path uses, under the teardown budget `MELODY_TEARDOWN_TIMEOUT` declares (`kernel.teardown_timeout`; left out, the application's `ApplicationConfig.TeardownTimeout`, then ten seconds; `unlimited` for no deadline), and takes exit code 1 when the budget runs out. Previously the clean path had no budget at all: one `Close` that never returned parked every service behind it and the process with them, so the healthy shutdown was the one without an emergency exit while the dying one had a way out.

**Symptom.** A process whose teardown blocks for longer than the budget now prints one line to stderr naming the abandoned step and exits 1, where it used to hang until the supervisor killed it.

**Remedy.** None required — the exit is the intended outcome. A deployment whose teardown legitimately takes longer raises `MELODY_TEARDOWN_TIMEOUT`; a service should still bound its own work, since the shield abandons the step, it does not shorten it.

### Logging: every fatal exit writes a certificate record at emergency level

**What changed.** The process-boundary exit handler writes one record at emergency level — "process exiting after unrecovered error", with the exit code and the error in its context — through whatever logger it resolved, always. The detailed record of the failure is still written at the error's own level, which an operator threshold can silently discard; the certificate is the one record no threshold drops.

**Symptom.** A log pipeline sees one new emergency record per fatal exit, beside the detailed record it may or may not have been keeping. A pipeline alerting on emergency level now fires on every fatal exit, which is what the level exists for.

**Remedy.** None for most deployments — the record is the signal that was missing. A pipeline that must not see it filters on the record's message, which is stable.

### Logging: the correlation id wins the context key

**What changed.** The correlation decorator — `NewRequestLogger` on the request path, the new `NewProcessLogger` on the console path — writes the real id under the context key unconditionally. A non-empty string already sitting under the key used to make the decorator keep it and drop the real id. On the request path a different non-empty string claim now survives under the key suffixed `Claimed`; on the console path the caller's value survives verbatim, whatever its type, under the key suffixed `Provided`.

**Symptom.** A record whose context carried its own `requestId`/`processId` value now shows the generated id under that key and the caller's value under the suffixed one. Anything grepping the log by the id melody generated now finds every record of the run, including the ones that used to escape the chain.

**Remedy.** A consumer that deliberately overrode the key should read its value back from the suffixed key; everything else needs no change — the correlation is simply no longer forgeable from a context value.

### Config: a late secret marking travels to the parameters assembled from the key

**What changed.** `MarkSecret` called after the boot resolution now propagates to every parameter whose template reads the marked key, and follows the marking to a fixpoint through derivation chains — exactly what a marking arriving before the resolution has always done. The scan reads the raw templates; a match inside doubled-percent escaped text over-marks, which errs toward redacting more, never less.

**Symptom.** `debug:parameters` redacts values it used to print: the dsn assembled from a password marked late now shows as secret beside the key itself. No stored value changes — the marking governs display, not storage.

**Remedy.** None. A parameter that must stay visible should not read a marked credential in its template.

### Httpclient: a basic credential travels whenever it was asked for

**What changed.** A `BasicAuth` with an empty username, `&httpclient.BasicAuth{Password: password}`, now sends the credential. The username guard used to drop the whole authorization silently, so an api key spelled as the password of an empty user — the shape of curl's `-u :key`, legal under RFC 7617 — produced an unauthenticated request presented as a success.

**Symptom.** A request built with an empty username and a non-empty password reaches the server with an `Authorization` header it never carried before.

**Remedy.** None for a correct caller. A caller that relied on the empty username to mean "no credential" passes no basic authorization at all.

### Httpclient: the response body cap binds the stream, the default included

**What changed.** `RequestStream` enforces `MaxResponseBodyBytes` — the cap was completely inert on the streaming path — and the inherited default of ten mebibytes now binds a stream whose caller never named a cap. Reading past the cap answers "response body exceeded max size" with the cap, the method and the sanitized url in the record. An invalid cap is also refused before anything is dialled, on both paths, so a POST no longer commits its side effect before being told its options were invalid.

**Symptom.** A stream that delivers more than ten mebibytes — a large download, a long-lived server-sent-event feed — errors mid-read where it used to run unbounded.

**Remedy.** Name the cap the stream actually needs through `RequestOptions.MaxResponseBodyBytes`; a genuinely unbounded consumer sets one sized far above its expected traffic, since unlimited is refused. Callers of the buffered `Request` are unaffected — the default has always bounded them.

### Opaque tokens: a stored token with no issue instant is refused once its user is revoked

**What changed.** A revocation is no longer an enumeration. [`security/contract.RevocationEpochStore`](../security/contract/token_store.go) publishes a boundary per user, and per device of a user, and [`Lookup`](../security/in_memory_token_store.go) refuses a token issued before the boundary that governs it. This closes the window [`DeleteByUser`](../../integrations/rueidis/v4/token_store.go) could never close: it walks an index with `SSCAN`, which does not promise to return a member added while the walk is in progress, so a token issued during a revocation survived it. The comparison needs an issue instant, so [`security/contract.Claims`](../security/contract/token_validator.go) carries `IssuedAt`, stamped by the store on every write.

The new methods, `RevokeBefore(runtimeInstance, userIdentifier, deviceIdentifier, instant) error` and `RevocationEpoch(runtimeInstance, userIdentifier, deviceIdentifier) (time.Time, error)`, live on their own interface, composed into `EpochRevocableTokenStore` rather than added to `RevocableTokenStore`, so a token store that publishes no boundaries need not implement them, and a caller that needs one is told so at the moment the service is asked for, by [`security.TokenStoreFromRuntime`](../security/bound_token_store.go) for code holding a runtime and by the rueidis integration's `EpochRevocableTokenStoreMustFromResolver` for a provider.

**Symptom.** A token stored by an earlier release carries no issue instant. The zero instant precedes every boundary, so the first time `RevokeBefore` is called for a user, that user's pre-upgrade tokens stop resolving — including ones an operator did not mean to end. Users nobody revokes are unaffected: with no boundary there is nothing to compare against and the token resolves exactly as before.

**Remedy.** None is needed in the ordinary case, and the behaviour is the safe direction: the tokens that stop resolving belong to an account somebody deliberately revoked. If an upgrade must not end any pre-upgrade session, do not call `RevokeBefore` until the longest token lifetime has passed since the deploy; every token written after it carries an instant and is compared normally.

Two consequences worth knowing before wiring it up. A token whose issue was in flight across a revocation is refused — the instant is stamped before the write reaches the store, so a token stamped just before the boundary and written just after it is treated as predating it. That is over-strict rather than under-strict, and deliberate. And the instants come from application clocks, so a node whose clock runs ahead of the node a revocation is issued from stamps tokens that read as later than the boundary and survive it: the window is exactly the skew between the two, and a single node whose clock steps backwards — an NTP correction, a restored snapshot, a resumed virtual machine — produces the same thing without any second node. `TokenStoreConfig.MaximumClockSkew` on the redis store, and `JwtConfig.RevocationEpochSkew` on the json web token path, bound that window: they widen the boundary by the stated amount and, on the store, additionally refuse a stamp further ahead of the verifying
node than the same amount. Both default to zero, which leaves the behaviour of this release unchanged; set them to the worst skew the fleet can carry. The cost is symmetrical and deliberate: a token issued within that window AFTER a revocation is refused too. `TokenStoreConfig.RevocationEpochRetention` is unrelated to any of this — it floors how long a boundary is kept when there is no index deadline to adopt, and does not affect the comparison.

### Security: `Claims` carries `IssuedAt` and `DeviceIdentifier`

**What changed.** [`security/contract.Claims`](../security/contract/token_validator.go) gains two fields: `IssuedAt`, the instant a revocation boundary is compared with, and `DeviceIdentifier`, which the jwt validator fills from its configured device claim and which a per-device boundary is looked up by.

**Symptom.** A `Claims` literal written positionally, without field names, stops compiling; keyed literals and every reader keep working. A token validator of your own that leaves both fields empty is answered by a revocation boundary as a token with no issue instant and no device.

**Remedy.** Write `Claims` literals with field names. A validator or token store of your own that takes part in revocation fills `IssuedAt` from the token's issue time and `DeviceIdentifier` from the device the token was issued to, so a per-device revocation reaches it.

### Messagebus: an unhandled consumed message fails the dispatch

**What changed.** A message whose type has no registered handler used to pass through the handle middleware with a warning and nil error; the consume command then Acked it. The default now refuses the dispatch, and the opt-in is `HandleMiddlewareConfig.AllowMissingHandler`, so the zero value is the safe cell. The deprecated `RequireHandler` field the third major kept is removed.

**Symptom.** On the consume path, a forgotten `RegisterHandler` line — or a handler registered for `T` while the transport decodes `*T` — used to drain the queue one warning at a time: every message Acked and destroyed, the retry, dead-letter and failure-transport machinery never engaging because the pipeline was told the message was handled. The same mistake now nacks into exactly that machinery and is impossible to miss. A `HandleMiddlewareConfig` literal naming `RequireHandler` does not compile.

**Remedy.** Delete `RequireHandler: true`: refusing is the default. A bus that genuinely wants pass-through (a tap that observes some types and forwards the rest) sets `AllowMissingHandler: true` and keeps the old behaviour, warning included.

### Mailer: configured smtp credentials fail closed when the server does not advertise AUTH

**What changed.** With a username configured, a server whose EHLO response does not advertise `AUTH` is refused — whatever `RequireAuthentication` says. `RequireAuthentication` keeps its other half: it still refuses the configuration in which authentication is required but no username is set.

**Symptom.** The old default skipped the whole auth branch and delivered the message as anonymous submission while reporting success, so the configured identity went quietly unused — most commonly against a relay that only advertises `AUTH` after `STARTTLS`, on a session where tls was not negotiated. Deployments in that shape now get an error naming the unapplied credentials instead of silent unauthenticated delivery.

**Remedy.** For a relay that genuinely takes unauthenticated submission, remove the credentials from the configuration — they were doing nothing. For a relay that advertises `AUTH` only after `STARTTLS`, leave `AllowPlaintext` out, the default, so the session upgrade is guaranteed before the auth branch is reached.

### Translation: an absent parameter stays visible, and a misnamed catalog file refuses the load

**What changed.** A plain placeholder whose parameter is absent renders as the visible placeholder itself (`Hello, {name}!`) instead of as an empty string; a parameter present with an explicit nil still renders empty. And `JsonDirectoryLoader.Load` answers a hard error naming any `.json` file that does not parse as `<domain>.<locale>.json`, instead of skipping it.

**Symptom.** Rendered empty, a renamed parameter key shipped every message quietly missing its amount, name or count, with nothing anywhere to learn it from; a golden test that asserted the empty rendering sees the placeholder now. A translations directory in the natural-but-unsupported `en.json` layout used to load zero catalogs successfully, with the runtime symptom — raw message ids in production — pointing nowhere near the mis-named files; that directory now fails the boot with the file named.

**Remedy.** Fix the parameter key, or pass the parameter with an explicit nil where the empty rendering was genuinely wanted. Rename catalog files to `<domain>.<locale>.json` — `messages.en.json` for the default domain — and delete stray `.json` files from the translations directory.

### Compile-level: `messagebus/contract.Transport`'s `Close` lost its runtime parameter

**What changed.** The contract method is `Close() error`; the former `Close(runtimeInstance Runtime) error` is gone, and `RegisterTransports` now registers a `TransportsCloser` the container's ordered teardown reaches — built at boot wherever the transports were registered, so a process that never resolves the transports map closes them too.

**Symptom.** A userland transport fails to compile against the interface until the parameter is deleted. The old signature was structurally dead: the teardown recognizes `Close() error` and nothing else, nothing in the framework or any production wiring ever called a transport's `Close`, so every broker connection lived exactly as long as the process and every deploy tore it down abruptly.

**Remedy.** Delete the parameter from the implementation. A transport that used the runtime for a deadline owns its bound now — the builtin amqp transport already carried its own join timeout and ignored the runtime entirely, which is what made the removal free.

### Container: `ScopeManager` and `Scope` declare scoped registration

**What changed.** A scope is a registrar of its own. [`container/contract.ScopedRegistrar`](../container/contract/scoped_registrar.go) declares `RegisterScoped(serviceName string, provider any, options RegisterOptions) error` and `MustRegisterScoped(...)`, which declare a service whose lifetime is one scope — one http request, one command run — built lazily on the first resolution through a scope and closed when that scope closes. [`container/contract.ScopeManager`](../container/contract/scope.go), and so `Container`, and `Scope` embed it: on the container a registration reaches every scope created afterwards, on a scope it adds a service to that one live scope. `Application.RegisterScoped` registers on the kernel's service container directly; the third major asserted the interface on the value and refused a container without it.

**Symptom.** An out-of-tree `Scope`, `ScopeManager` or `Container` no longer compiles until it declares `RegisterScoped` and `MustRegisterScoped`.

**Remedy.** A container or a scope held as its contract registers a scoped service directly, and the package generics `container.RegisterScoped` and `container.MustRegisterScoped` take it as their first argument:

```go
container.MustRegisterScoped(serviceContainer, "app.request.report", newRequestReport, container.RegisterOptions{})
```

An implementation of your own declares the two methods: it holds the providers apart from the instances it already keeps, builds one instance per scope on first resolution, and closes what it built when the scope closes. The framework's own implementation is the reference; see [`package/CONTAINER.md`](./package/CONTAINER.md) for what the two lifetimes may read from each other.

### Session: id rotation is `Manager.RegenerateSession`

**What changed.** [`session/contract.Manager`](../session/contract/manager.go) declares `RegenerateSession(runtimeInstance containercontract.Runtime, session Session) (Session, error)`, the session-fixation defence: it mints a fresh id, carries the values over, removes the entry the previous id pointed at, and latches the session passed in out of use. The framework's own [`session.Manager`](../session/manager.go) implements it, and [`http.RegenerateRequestSession`](../http/session.go) calls it on the registered manager and rotates and republishes in one call. The sibling `SessionRegenerator` the third major asserted is removed.

**Symptom.** An out-of-tree `session/contract.Manager` — a Redis-backed or database-backed session manager, say — no longer compiles until it declares `RegenerateSession`.

**Remedy.** Implement `RegenerateSession` on the manager. It has to refuse an id a logout or an earlier rotation already ended, mint an id the storage does not already hold, carry the values over, delete the previous entry and put the session it was given out of use, so that a caller who forgets to republish the rotated session is logged out cleanly instead of being left presenting a deleted id. The refusal is the defence itself: without it, a copy of the session loaded before a logout is rotated into a fresh id carrying the identity the logout ended, and two concurrent rotations of one id fork it into two live sessions. The check, the delete and the record that the id is now ended have to be one critical section per id, the way the framework's [`session.Manager`](../session/manager.go) holds its per-id lock across them and answers `ErrSessionDeleted`, with `ErrSessionRotated` beside it for an id a rotation ended. The excerpt below leaves that to the manager's own `DeleteSession`;
it is a sufficient implementation only where that call refuses an ended id atomically, and otherwise the framework's `RegenerateSession` is the reference to follow:

```go
type CustomSessionManager struct {
    /* the embed stands for the rest of the implementation this excerpt does not repeat */
    sessioncontract.Manager
}

func (instance *CustomSessionManager) RegenerateSession(
    runtimeInstance containercontract.Runtime,
    sessionInstance sessioncontract.Session,
) (sessioncontract.Session, error) {
    rotatedSession, mintErr := instance.NewSession(runtimeInstance)
    if nil != mintErr {
        return nil, mintErr
    }

    for key, value := range sessionInstance.All() {
        rotatedSession.Set(key, value)
    }

    deleteErr := instance.DeleteSession(runtimeInstance, sessionInstance.Id())
    if nil != deleteErr {
        return nil, deleteErr
    }

    sessionInstance.Clear()

    return rotatedSession, nil
}
```

The rotated-away `Session` is cleared, and the framework's own `Clear` latches: a caller that rotated and then kept writing to the original object cannot make it live again, so the response path cannot re-create the just-deleted id and re-issue it as the cookie. An out-of-tree `Session` implementation is cleared through its own `Clear()`, which latches only if that implementation makes it; an application whose `Session` does not latch must therefore not write to the object it rotated away.

See [`package/SESSION.md`](./package/SESSION.md) for what a rotation has to guarantee.

### Session: `RegenerateSession` returns a failed id probe instead of panicking

**What changed.** [`Manager.RegenerateSession`](../session/manager.go) returns a storage failure met while probing the fresh id as an error naming the door, with `sessionReference` and the failure as its cause, where it panicked with the raw failure. The fresh id is minted before the previous entry is removed, so the session in use stays intact. `NewSession` answers the same failure as its error, marked; see the section "Session: `Manager.Session` and `NewSession` answer an error".

**Symptom.** A storage outage during a rotation surfaces as the error `RegenerateSession` (and `http.RegenerateRequestSession`) returns, not as a panic a recovery caught.

**Remedy.** Check the error before publishing the rotated session, and keep the request on its current session when it fails; a `recover` around the call no longer sees this failure.

### Configuration: the four http settings added after v3.13.0 sit on `HttpConfiguration`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares the four http settings the third major added after v3.13.0, and the framework's own configuration implements them, reading each from its key:

- `StaticExcludedPaths() []string` — `MELODY_STATIC_EXCLUDED_PATHS` (`kernel.static.excluded_paths`), the path prefixes the built-in file server declines before it looks at the disk, a comma-separated list empty by default. Since the built-in file server sits outermost in the pipeline, excluding a prefix is how an application takes a part of the url back — to put authentication in front of a directory, or to serve it from a root of its own.
- `SessionTtl() bound.Resolved[time.Duration]` — `MELODY_HTTP_SESSION_TTL` (`kernel.http.session_ttl`), how long a stored session stays valid; 24h by default, `unlimited` for no expiry, and a value below one second fails the boot.
- `SessionTombstoneRetention() bound.Resolved[time.Duration]` — `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` (`kernel.http.session_tombstone_retention`), how long a deleted session id keeps refusing a write-back, five minutes by default; a value below one second and `unlimited` fail the boot, because a window that refuses nothing is not a shorter window but a disarmed logout defence.
- `ShutdownTimeout() bound.Resolved[time.Duration]` — `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`), see the shutdown section below.

The sibling `ExtendedHttpConfiguration` the third major asserted is removed.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` — a test double, or a configuration assembled in code rather than from `.env` artifacts — no longer compiles until it declares the four; the third major read such a type with the defaults.

**Remedy.** Implement the four, answering no excluded path and the resolved `config.DefaultSessionTtl`, `config.DefaultSessionTombstoneRetention` and `config.DefaultHttpShutdownTimeout` where the configuration sets nothing, and the server limit methods of the section on the limit keys below. Return a copy of the excluded paths rather than the field itself: the configuration is read on every request while the caller is free to keep the slice it was handed.

```go
func (instance *CustomHttpConfiguration) StaticExcludedPaths() []string {
    return append([]string{}, instance.staticExcludedPaths...)
}
```

### Routing: a non-final optional parameter without a default is refused at registration

**What changed.** An omitted optional parameter is dropped wherever it sits in the pattern, while a match only ever ends early at the tail. A pattern such as `/blog/:locale?/posts` therefore let [`UrlGenerator.GeneratePath`](../http/url_generator.go) mint `/blog/posts`, a path this very router answered with `404`. Such a pattern is now refused at the definition site by [`rejectNonTrailingOptionalParameter`](../http/router.go). A mid-pattern optional that carries a **non-empty default** is still accepted, because the default is always substituted and the segment is therefore never dropped.

**Symptom.** The application no longer boots. Registration panics with `optional route parameter must be the last pattern segment unless it has a default`, and the exception context names the offending `pattern` and `parameterName`.

**Remedy.** One of three, depending on what the route meant:

```go
/* refused at registration: the optional parameter is not the last segment and carries no default */
router.Handle("/blog/:locale?/posts", blogHandler, http.RouteOptions{Methods: []string{nethttp.MethodGet}})

/* accepted: the optional parameter is the last segment */
router.Handle("/blog/posts/:locale?", blogHandler, http.RouteOptions{Methods: []string{nethttp.MethodGet}})

/* accepted: a mid-pattern optional whose non-empty default is always substituted */
router.Handle(
    "/blog/:locale?/posts",
    blogHandler,
    http.RouteOptions{
        Name:     "blog.posts.localized",
        Methods:  []string{nethttp.MethodGet},
        Defaults: map[string]string{"locale": "en"},
    },
)

/* accepted: the long and the short pattern registered as two routes */
router.Handle("/blog/posts", blogHandler, http.RouteOptions{Name: "blog.posts", Methods: []string{nethttp.MethodGet}})
router.Handle("/blog/:locale/posts", blogHandler, http.RouteOptions{Name: "blog.posts.locale", Methods: []string{nethttp.MethodGet}})
```

An empty default does not lift the refusal: it would emit an empty segment, which no longer satisfies a parameter.

### Routing: a non-empty route default fills in for a parameter supplied empty

**What changed.** [`UrlGenerator.GeneratePath`](../http/url_generator.go) substitutes a non-empty route default for a parameter supplied with an **empty** value, not only for an absent one. A non-trailing optional segment is admitted at registration precisely because its default keeps the segment present, but generating `/:locale?/list/:page` with `{"locale": "", "page": "2"}` dropped it and produced `/list/2` — which this router answers with a `404`, the generator and the matcher disagreeing on the one class of pattern the registration guard newly admits.

**Symptom.** A call that passed an empty string for a parameter that has a non-empty default now gets the default in the path instead of an omitted segment: `/en/list/2` where it used to be `/list/2`. A **required** parameter supplied empty is likewise filled from a non-empty default instead of failing with `route parameter may not be empty`.

**Remedy.** Nothing, in the normal case — the natural caller passes the current locale, which is sometimes `""`, and now gets a path the router actually serves. A caller that relied on an empty value dropping the segment cannot get it back by omitting the parameter: an absent parameter takes the route default as well. For a **trailing** optional, give the route no default (or an empty one), which leaves the old behaviour — the segment is dropped whether the value is empty or absent, and a required parameter supplied empty is still refused. A **non-trailing** optional is admitted at registration only with a non-empty default, so it always generates its segment; declare the path without that segment as a route of its own and generate from it.

### Routing: an empty single-segment wildcard takes its default or fails

**What changed.** `UrlGenerator.GeneratePath` treats a single-segment `*name` wildcard as it treats a `:name` parameter: a value supplied empty takes a non-empty route default, and with none left the generation fails with `wildcard parameter may not be empty`, where the empty value was emitted as an empty segment, a `//` url the kernel refuses before it routes.

**Symptom.** Generating a path for a `*name` route with an empty value returns that error, or the default in the path, where it returned a url the router never served.

**Remedy.** Supply a non-empty segment for the wildcard, give the route a non-empty default for it, or handle the generation error.

### Middleware: equal-priority middlewares run in registration order

**What changed.** [`orderDefinitions`](../http/middleware/pipeline/builder.go) breaks a priority tie on the registration rank instead of on the definition's generated name. The generated name carries the registration counter as decimal text, so a lexicographic tie-break read it as `1, 10, 11, 2` and sorted every factory-provided middleware ahead of every directly registered one. Explicit priorities and `before`/`after` edges decide the order exactly as before.

**Symptom.** The pipeline nests differently. A middleware that used to run outside another may now run inside it, and the reverse. The visible case is a cors middleware registered before an authentication factory at the same priority: it used to end up **inside** the factory, so a preflight was answered `401` with no `Access-Control-Allow-Origin`; it now runs outside it, as the registration order asked.

**Remedy.** If the old nesting was load-bearing, say so explicitly rather than leaning on registration order. A lower priority runs further out:

```go
func (instance *ExampleHttpMiddlewareModule) RegisterHttpMiddlewares(
    kernelInstance kernelcontract.Kernel,
    registrar applicationcontract.HttpMiddlewareRegistrar,
) {
    /* a lower priority runs further out, so cors wraps authentication whatever order the two are registered in */
    registrar.Use(applicationcontract.MiddlewareRegistration{Priority: -100, Middlewares: []httpcontract.Middleware{cors.Middleware(cors.NewService(cors.Config{}))}})
    registrar.Use(applicationcontract.MiddlewareRegistration{Priority: 0, Middlewares: []httpcontract.Middleware{authenticationMiddleware()}})
}
```

`Before`/`After` edges live on [`pipeline.HttpMiddlewareDefinition`](../http/middleware/pipeline/definition.go) for a pipeline assembled directly through [`pipeline.NewBuilder`](../http/middleware/pipeline/builder.go); the module registrar exposes priority. [`(*HttpMiddleware).LastBuildReport`](../application/http_middleware.go) reports the order a serving process actually built; `debug:middleware` lists the pipeline through its own description pass, leaving that report alone.

### Object storage: `awss3` `Put` enforces the declared size

**What changed.** [`Storage.Put`](../../integrations/aws/s3/v4/storage.go) proves the body against the `size` it was given *before* anything can be committed at the key, and never holds more than 16 MiB in memory doing it. It previously uploaded first and probed the caller's reader afterwards.

**Symptom.** A call that declared a size **shorter** than the body used to report success and leave a truncated object at the key; it now fails with `storage object size does not match the declared size`, naming the key and the declared size, and nothing reaches the key.

Which byte the failure happens at depends on the body:

- A seekable body (`*bytes.Reader`, `*strings.Reader`, `multipart.File`, `*os.File`) is measured in place, so the call fails before a single request is issued.
- A body that cannot seek and is declared at or below 16 MiB is drained and validated in full first, so it also fails before the bucket is touched.
- A body that cannot seek and is declared **above** 16 MiB is checked as MinIO consumes it, so the failure lands partway through the upload: the reader stops one byte short of the declared length, MinIO's multipart upload is aborted, and nothing is visible at the key.

**Remedy.** Declare the real size, or pass `-1` for a stream of unknown length, which is uploaded unchecked:

```go
/* the declared size is enforced, so declare what the body really holds */
putErr := objectStorage.Put(
    runtimeInstance,
    "invoice/2026-07.pdf",
    bytes.NewReader(document),
    int64(len(document)),
    storagecontract.PutOptions{ContentType: "application/pdf"},
)
if nil != putErr {
    return putErr
}

/* a stream of unknown length declares -1 and is uploaded unchecked */
return objectStorage.Put(
    runtimeInstance,
    "upload/report.csv",
    body.Body,
    -1,
    storagecontract.PutOptions{ContentType: "text/csv"},
)
```

A correct size, a zero declared size, and a body **shorter** than its declared size all behave exactly as before. The same pass also stopped reading a legal `(0, nil)` read as the end of the body — which let an over-read go undetected and stored a silently truncated object — and bounds consecutive empty reads while honouring the runtime context, so a stalled body or a client that walked away fails the put instead of pinning a core and an upload.

### Validation: a nil pointer embed is validated as "nothing was supplied"

**What changed.** [`dereferencedValidationStructValue`](../validation/validator.go) yields the zero embed for a nil pointer embed, so the constraints its promoted fields declare run against their zero values exactly as a value embed's already did.

**Symptom.** A request that mentioned no field of a `*T` embed is now rejected with the constraint errors the embed's tags declare. It used to pass: naming any sibling field made `encoding/json` allocate the embed and re-arm the constraints, so a body of `{"status":"open"}` on a request whose `*Audit` embed declares `ActorId` as `notBlank` was accepted and then dereferenced nil in the handler.

**Remedy.** Supply the fields, or stop declaring constraints on an embed the payload is allowed to omit. A promoted field shadowed by an outer field of the same json name stays unvalidated, so the `encoding/json` dominance rules are unchanged.

### Validation: nesting past the depth cap is a validation error when the subtree could carry a tag

**What changed.** Exceeding the nesting-depth cap is reported as [`ErrorNestingDepthExceeded`](../validation/const.go) (`nestingDepthExceeded`) when the truncated subtree could actually carry a `validate` tag, and passes silently when it could not. The walk previously returned an empty error list past the cap, which `Validate` converted to `nil`, so nesting a payload one level deeper than the cap bypassed every constraint in it. The reachability check follows pointers, slices, arrays and map elements and is memoized per type; the cap value is unchanged.

**Symptom.** A deeply nested payload that used to validate now fails with a `nestingDepthExceeded` error naming the field. Tag-free free-form client json — a `map[string]any` metadata field, for example — is still accepted at any depth.

**Remedy.** Flatten the request type, or keep the deep part of the payload tag-free so nothing below the cap declares a constraint.

### Validation: a parameterized constraint is constructed once and shared

**What changed.** The parsed `validate` tag and the constraint a parameterized rule resolves to are memoized instead of being rebuilt for every value the validator reaches — a `regex` tag recompiled its pattern once per element. The parse cache is keyed on the tag string, the constraint cache on the rule name and its parameters, and the constraint cache is per-validator, so custom constraints registered under the same name in different validators stay separate.

**Symptom.** A custom [`contract.ParameterizedConstraint`](../validation/contract/constraint.go) whose `WithParameters` result carried per-value state, or was not safe for concurrent use, now leaks that state between unrelated values and unrelated requests.

**Remedy.** Make the constraint `WithParameters` returns immutable and safe for concurrent use; do not retain the params map it was handed, and do not accumulate state in `Validate`. One instance is shared for the process lifetime across every request and goroutine that reaches the rule.

### HTTP kernel: `KernelConfig.SessionCookiePolicy` keeps the `SameSite=Lax` default

**What changed.** [`resolveSessionCookieSameSite`](../http/router_utility.go) treats the zero `SameSite` as unset and falls back to `Lax`, the same way an empty `Path` falls back to `/`.

**Symptom.** A policy that named only `Path` or `Domain` used to emit no `SameSite` attribute at all; it now emits `SameSite=Lax`.

**Remedy.** None, unless the omission was deliberate — `nethttp.SameSiteDefaultMode` remains the way to ask for no attribute on purpose.

A policy that asks for `SameSite=None` gets a `Secure` session cookie under `SessionCookieSecureFromScheme` whatever the request's scheme, since every current browser drops a `None` cookie that is not `Secure`. Over plain http the browser refuses that cookie, so a development setup served without TLS uses `SameSite=Lax`, the default, or `Strict`, and keeps `None` for https. `SessionCookieSecureNever` beside `None` is refused when the kernel is built.

### HTTP kernel: the session saved is the one published on the request

**What changed.** The response path reads the session published with [`http.SetRequestSession`](../http/request_session.go) at the moment the response is written, preferring the session a handler published over the one a read loaded.

**Symptom.** Publishing a session in a handler now takes effect: the published session is what gets stored and what the `Set-Cookie` advertises. It used to be discarded.

**Remedy.** None for a handler that wanted that.

### HTTP: `JsonHandler` rejects a literal `null` body

**What changed.** [`JsonHandler`](../http/typed_handler.go) answers `400` for a literal `null` request body when its request type is a kind a json `null` leaves nil — a pointer, a map, a slice, an interface, a channel or a function. The four-byte body decoded without error and left the value nil, the validator took its nil-pointer early return and reported every constraint satisfied, and the handler then dereferenced nil.

**Symptom.** That request is now a client error instead of a `500`.

**Remedy.** None. A struct value instantiation and a `{}` body were never affected, and a caller-supplied `JsonHandlerConfig.ErrorResponder` still shapes the response.

### HTTP client: `TransportConfig` fields are `bound` limits, and the two idle pools are decided each on its own

**What changed.** Every [`TransportConfig`](../httpclient/transport_config.go) field is a [`bound.Duration`](../bound/limit.go) or a [`bound.Count`](../bound/limit.go): a field left out takes its default, `bound.Of` sets a value, and a value below the field's minimum is refused by `NewHttpClient`, naming the field. The third major kept value fields and read a non-positive value as not set. `IdleConnectionTimeout`, `MaxIdleConnections` and `MaxIdleConnectionsPerHost` admit `bound.UnlimitedDuration()` / `bound.UnlimitedCount()`; every timeout but `IdleConnectionTimeout` refuses it, the dial, TLS handshake, response header and 100-continue waits since each guards against a peer that never answers; `ExpectContinueTimeout` admits `bound.Of(time.Duration(0))`, which sends the body at once; `KeepAliveDisabled` turns the keep-alive probes off. `MaxIdleConnections` and `MaxIdleConnectionsPerHost` both default to 100, as in the third major, but each is now decided on its own: the third major copied a `MaxIdleConnections` set to a
positive value into the per-host pool unless that was set too, and the fourth leaves the per-host pool at its default; net/http applies both pools and the smaller one wins. `IdleConnectionsDisabled` is new: it keeps no connection idle over HTTP/1 and HTTP/2 alike, every HTTP/1 request carrying `Connection: close`, and refuses `MaxIdleConnections`, `MaxIdleConnectionsPerHost` or `IdleConnectionTimeout` set beside it; the third major had no way to turn pooling off.

**Symptom.** A `TransportConfig` literal with value fields, `TransportConfig{DialTimeout: 5 * time.Second}`, no longer compiles. A field set to zero or below is refused naming the field, where it read as the default, `ExpectContinueTimeout` excepted, whose zero is a value. A client that raised `MaxIdleConns` alone, to 500 say, keeps at most 100 idle connections per host where the third major kept 500, so a single busy upstream sees more connections opened and closed.

**Remedy.** Wrap each set field: `TransportConfig{DialTimeout: bound.Of(5 * time.Second), MaxIdleConnections: bound.Of(50)}`, and leave out a field that was set to zero to take the default. A caller who raised `MaxIdleConns` for one busy host sets `MaxIdleConnectionsPerHost` to the same value to keep the third major's pool; a caller who wants no connection reuse sets `IdleConnectionsDisabled: true`.

### CLI: json mode writes the document and nothing else

**What changed.** In json mode the ansi start/finish banner that [`cli.Register`](../cli/command.go) wraps around every registered command is suppressed, and `--format=json` implies `--no-color` through [`NormalizeOption`](../cli/output/option_parser.go).

**Symptom.** `jq` and `json.Unmarshal` now consume `debug:*` output directly instead of failing on the first byte. A consumer that scraped the banner off stdout finds it gone.

**Remedy.** Read the envelope. `meta` already reports the command, its arguments, the start time and the duration, and `error` reports the final status.

### CLI: a command whose envelope reports an error exits non-zero

**What changed.** [`output.Render`](../cli/output/renderer.go) returns an exit-coded error after writing the envelope. A registered service that errors or panics while being constructed is reported as `debug.buildFailed` rather than `debug.notFound`.

**Symptom.** A command that reported an error in its payload while exiting `0` now exits `1`. `debug:container <name>` fails when the service cannot be resolved instead of printing `[success]`.

**Remedy.** Nothing to change in the framework. A wrapper script that treated a zero exit as success was reading a status that was never true; a deployment gate such as `app debug:container app.repository.order || exit 1` now works as written. A command of your own that renders a non-nil `Envelope.Error` deliberately and still wants a zero exit must not put the failure in the envelope.

### CLI: `--format` and `--order` reject an unrecognised value

**What changed.** Both flags carry a validator ([`StandardFlags`](../cli/output/standard_flag.go)), so `--format=JSON`, `--format=yaml` and `--order=ascending` fail during flag parsing with a message naming the accepted values, matching how `--limit=abc` already behaved.

**Symptom.** A script passing an unsupported value now fails with a non-zero exit instead of quietly receiving the human table.

**Remedy.** Pass `table`, `json` or `json-pretty`, and `asc` or `desc`. Omitting either flag still defaults to `table` and `asc`.

### CLI: `--limit`, `--offset` and `--order` are applied to the rendered items

**What changed.** `debug:router`, `debug:events`, `debug:parameters`, `debug:middleware` and `debug:container` apply the window through [`output.WindowItems`](../cli/output/list_payload.go) and the order through [`output.ApplySortOrder`](../cli/output/list_payload.go), reversal running before the window so a descending window returns the end of the list. `total` continues to report the unwindowed count.

**Symptom.** An invocation already passing `--limit` or `--offset` received the full list and now receives a window; with `--verbose`, `debug:events` also narrows its listeners block to the windowed events. `--order=desc` was accepted and ignored before, so an invocation that passed it now gets different output.

**Remedy.** Nothing for a client that paged with `offset += limit` — it now walks each item exactly once instead of re-reading the whole list on every page. A consumer that passed `--limit` while expecting everything must drop the flag.

### Distributed lock: the callback is told to stop a quarter of the ttl before the lease lapses

**What changed.** [`RunExclusive`](../lock/run_exclusive.go) and [`LeaderGate`](../lock/leader_gate.go) demote on the lease's demotion instant, half a renewal cadence — a quarter of the ttl at the default cadence — before the lease last written lapses, if no renewal has landed by then. A timer arms it whatever the store is doing — each renewal runs on a goroutine of its own, so one that ignores its context cannot hold it back — every renewal is bounded to it, and a failed renewal is retried once, halfway to it. A renewal that hung until its whole-cadence deadline or a store that failed at once used to demote at the lapse itself — the instant a contender could acquire — so the callback and the new holder overlapped.

**Symptom.** A renewal that answers after the demotion instant no longer saves the lease: the callback's runtime is cancelled and `RunExclusive` returns `exclusive run lost the lock lease while running`, where a renewal landing anywhere inside the lease used to keep it. With a 30-second ttl the renewal issued at 15 seconds has until 22.5 seconds to land.

**Remedy.** None where the store answers renewals within a quarter of the ttl. A store that is regularly slower needs a longer ttl, which widens every window proportionally; for a `LeaderGate`, a denser `RefreshInterval` also narrows the demotion margin, since the margin is half the cadence. Session mode (an unlimited ttl) is unchanged.

### Distributed lock: `LeaderGate.OnElected` receives a term-scoped runtime

**What changed.** [`LeaderGate`](../lock/leader_gate.go) starts renewing its lease **before** `OnElected` runs, and hands the hook a runtime whose context is cancelled when the lease is lost. Nothing renewed the lease while the hook ran, so a hook slower than the ttl let the lease lapse: another replica acquired it, both reported leadership, and the incumbent never demoted, because demotion only follows a failed renewal — which could not happen while the hook held the campaign loop.

**Symptom.** Leader-only work started inside `OnElected` that respects its context now stops when the lease is lost, instead of running alongside the new holder. The runtime the hook receives is no longer the run runtime, so its context ends at the end of the term rather than at the end of the process.

**Remedy.** If the hook needs a context that outlives the term — a cleanup that must finish whatever happens to the leadership — derive it from the run runtime captured outside the hook rather than from the one the hook is handed. Work that is only correct while this replica is leader should keep using the hook's runtime, which is the point of the change.

### Message bus: `melody:messagebus:consume` separates the signal from the handler lifetime

**What changed.** [`ConsumeCommand`](../messagebus/consume_command.go) runs the delivery pull and the handlers on two contexts. The shutdown signal stops the pull; in-flight handlers and their `Ack`/`Nack` keep a live context for the whole [`ConsumeCommandConfig.ShutdownTimeout`](../messagebus/consume_command.go) window (30 seconds when left out), and the grace expiring is what cancels them.

**Symptom.** A handler that respects its context is no longer cancelled the instant the signal arrives; it is cancelled when the grace expires. One shared context meant the grace protected nothing, and the acknowledgement ran on the cancelled context too — so any transport honouring the runtime context on publish failed the `Ack` of a message whose side effects had already committed, the broker redelivered it on every deploy, and a failed `Nack` dropped the `RedeliveryStamp` increment so `MaxRetries` never converged.

**Remedy.** Size the grace to the slowest handler with `ConsumeCommandConfig{ShutdownTimeout: bound.Of(grace)}` on either constructor, and make sure a handler that must not be interrupted mid-write finishes inside it. A handler that relied on being killed at the signal now runs to completion or to the grace deadline.

### Websocket: an `IdleTimeout` the keepalive cannot work with is refused at construction

**What changed.** [`websocket.NewStreamHandler`](../../integrations/websocket/v4/handler.go) refuses a `StreamHandlerConfig.IdleTimeout` below a second or unlimited, naming `StreamHandlerConfig.IdleTimeout`, and reads an absent one as 30s; the third major refused every non-positive value instead of treating it as "no keepalive". Nothing else in the stack can reap a peer that goes away without a fin: `coderwebsocket.Accept` hijacks the connection, so `http.Server`'s read and write timeouts stop applying to it; the read loop then blocks in `Read` with no deadline of its own; and a write into a half-open socket keeps succeeding for as long as the send buffer has room, so a broadcast is no liveness signal either. The keepalive ping is the only remaining evidence, which is why it has no off position.

**Symptom.** A handler built with `IdleTimeout: bound.Of(0)`, a sub-second interval or `bound.UnlimitedDuration()` panics at construction with `the limit is below its minimum` or `the limit does not admit unlimited`, the context naming `StreamHandlerConfig.IdleTimeout`. Through [`websocket.NewModule`](../../integrations/websocket/v4/module.go) the handler is built while routes are registered, so the failure surfaces at boot rather than on the first upgrade request. Options that name no `IdleTimeout` ping every 30s.

**Remedy.** Leave `IdleTimeout` out for the 30s a browser client suits, which answers the ping inside its protocol stack where the page's JavaScript never sees it, so a receive-only client stays connected; or name another interval of at least a second:

```go
websocket.NewModule(websocket.ModuleConfig{
    Streams: []websocket.StreamConfig{
        {
            Hub:  hub,
            Path: "/ws",
            Handler: websocket.StreamHandlerConfig{
                TopicResolver: topicResolver,
                IdleTimeout:   bound.Of(15 * time.Second),
            },
        },
    },
})
```

### Other integration modules

- **`bunorm` deterministic encryption.** `melody:encrypt:database --mode=encrypt --deterministic` ([`encrypt_database_command.go`](../../integrations/bunorm/v4/encrypt/encrypt_database_command.go)) now rewrites a column that was already bulk-encrypted with random nonces into its deterministic form, keeping the key each value already carries ([`migrate.go`](../../integrations/bunorm/v4/encrypt/migrate.go)). Every such value previously authenticated under a live key and was passed through untouched, so the command reported success while converting nothing and every [`CiphertextCandidates`](../../integrations/bunorm/v4/encrypt/cipher.go) equality lookup on that column returned zero rows. *Symptom:* a deterministic run over an already-encrypted column now writes rows where it used to write none. *Remedy:* none — it remains idempotent and never rotates keys, so `--mode=reencrypt --target-key=...` is still the only way to change a key.
- **`bunorm` audit change-sets.** [`audit.ChangeSet`](../../integrations/bunorm/v4/audit/change.go) always serialises an empty change-set as `[]` rather than the json literal `null`. *Symptom:* a trail consumer that special-cased `null` in the `changes` column sees `[]` instead. *Remedy:* drop the special case; `jsonb_array_length(changes::jsonb)` now reads `0` where it errored or read `1`.
- **`bunorm` encrypted columns and json.** The four encrypted column types ([`encrypted_string.go`](../../integrations/bunorm/v4/encrypt/encrypted_string.go)) gain an `UnmarshalJSON` that refuses the redaction placeholder `<redacted>`. Until now a json document rendered from a model — where `MarshalJSON` redacts — and decoded back into the model stored the placeholder as the column value, and the next write sealed `<redacted>` in place of the secret, silently. *Symptom:* decoding such a document now fails with a message that opens with `a redacted encrypted value cannot be decoded back into an encrypted column`, naming the column type in the context; a plaintext string still decodes as itself and a `null` leaves the value untouched. *Remedy:* do not post a redacted rendering back into the model — carry the secret separately, or read the stored value through the column and leave the field out of the document.
- **`bunorm` encrypted columns and numeric verbs.** The four encrypted column types ([`encrypted_string.go`](../../integrations/bunorm/v4/encrypt/encrypted_string.go)) implement `fmt.Formatter`, so the numeric verbs (`%d`, `%o`, `%b`, `%c`, `%U`) can no longer bypass the redaction and print the plaintext. `String`, `LogValue` and `MarshalJSON` already redacted, but a numeric verb reaches none of them and printed the underlying string through fmt's badverb form. *Symptom:* `fmt.Sprintf("%d", someEncryptedColumn)` now renders `<redacted>` instead of the plaintext. *Remedy:* none.
- **`bunorm` deterministic columns.** `EncryptDeterministic` and `EncryptDeterministicWithKeyId` ([`cipher.go`](../../integrations/bunorm/v4/encrypt/cipher.go)), and through them `EncryptedDeterministicString` and `EncryptedDeterministicStringFor[R]`, no longer pass a random-nonce seal through: it is re-sealed deterministically under the key id it already carries. *Symptom:* a deterministic column handed a value sealed by `Encrypt` (a value copied from a random column, or sealed before the column type changed) now stores a ciphertext `CiphertextCandidates` finds, where it used to store one the equality lookup could never match. *Remedy:* none; a deterministic seal, under any key still in the set, passes through as before and the random doors are unchanged.
- **`bunorm` all-zero encryption key.** `NewStaticKeyProvider` ([`key_provider.go`](../../integrations/bunorm/v4/encrypt/key_provider.go)) panics at construction on a key of 32 zero bytes, naming the key id, as it already did on a key of the wrong length. *Symptom:* a process whose key material was never generated — a zeroed buffer, an unset variable decoded and padded — refuses to boot instead of sealing every column under a key any reader can guess. *Remedy:* generate the key from `crypto/rand`; a key of any other content, however weak, is not judged.
- **`bunorm` async audit storage.** [`audit.AsyncStorage.Save`](../../integrations/bunorm/v4/audit/async_storage.go) returns an error wrapping `ErrAsyncStorageQueueFull` or `ErrAsyncStorageClosed` for an entry its queue could not take, where it answered `nil`; the entry is still dead-lettered, and the error names every refused entry under `refused` with its position in the call. *Symptom:* a request path that propagates the audit write's error fails when the queue is full or the storage is closing. *Remedy:* match the two sentinels with `errors.Is` and decide per call whether a lost audit record fails the request or is only journalled.
- **`bunorm` audit change presence.** A [`audit.Change`](../../integrations/bunorm/v4/audit/change.go) the tracker builds marshals each side by presence: a transition to a zero value renders it (`"new":false`, `"new":0`, `"new":""`), where `omitempty` left that side out and made it read like an insert or a delete. A `Change` built by hand keeps the old reading. *Symptom:* a consumer of the `changes` column that took an absent side for "no new value" now sees the zero. *Remedy:* read a present key as the value it carries and an absent key as no value recorded.
- **`bunorm` audit table name.** [`audit.BunStorage.Save`](../../integrations/bunorm/v4/audit/storage.go) refuses, as an error, a table name that is not a plain identifier, the empty name included, since the name reaches the insert as raw SQL; it used to substitute the default table for `""`. *Symptom:* a direct caller that passed `""` gets `audit table name is not a valid identifier` instead of a write to the default table. *Remedy:* pass `audit.DefaultTable`, or the name the registry hands out.
- **`bunorm` audited delete.** [`Tracker.Delete`](../../integrations/bunorm/v4/audit/track.go) records the identifier, the actor and the time, and no field values, unless the entity opts in with [`EntityOptions.CaptureDeleteBeforeImage`](../../integrations/bunorm/v4/audit/config.go), which loads and locks the row before removing it; a delete that matched no row is not recorded. *Symptom:* a delete entry carries no old values where it carried the fields of the model handed in, and a delete of an absent row leaves no entry. *Remedy:* set `CaptureDeleteBeforeImage: true` on every entity whose deleted contents must be recoverable from the trail; with it, deleting a row that is already gone is an error.
- **`bunorm` pgsql duplicate key.** [`pgsql.IsDuplicateKey`](../../integrations/bunorm/pgsql/v4/pgsql_error.go) answers on the typed SQLSTATE `23505` a PostgreSQL protocol error carries, read through the wrapping, instead of searching the error's text. *Symptom:* an error that carries no protocol error — a hand-written message containing "duplicate key", an error stringified across a process boundary — no longer matches. *Remedy:* keep the driver's error in the chain (`%w`, or an exception's cause) where the check has to see it.
- **`cron` busybox day fields.** The user-less crontab dialect ([`template_crontab.go`](../../integrations/cron/v4/template_crontab.go)) refuses, with `ErrBusyboxDivergentDaySchedule`, a day-of-month and day-of-week pair that busybox crond runs as a different schedule than vixie crond and the in-process runner. *Symptom:* `melody:cron:generate` fails under that dialect for an entry it used to render. *Remedy:* restate the pair so both daemons read it the same, usually by leaving one of the two day fields at `*`.
- **`cron` shutdown of a running job.** The runner's shutdown gives a running command the entry's `ShutdownTimeout` (the runner default is five minutes) to unwind after cancelling its context, then abandons it with its scope closed and reports it at warning ([`runner_command.go`](../../integrations/cron/v4/runner_command.go)); it used to wait for it without a bound when the entry set no `Timeout`. *Symptom:* a job that ignores its cancelled context no longer holds `melody:cron:run` or `--once` open past that window. *Remedy:* have the command return when its context is cancelled, or set a `GracefulTimeout` sized for its unwinding.
- **`rueidis` call timeout.** Every round trip of the [`Locker`](../../integrations/rueidis/v4/lock.go), the [`NonceGuard`](../../integrations/rueidis/v4/nonce_guard.go), the [`RedisTokenStore`](../../integrations/rueidis/v4/token_store.go) and the server-sent event backplane is bounded by a call timeout of one second by default, capping the caller's context; reads used to run unbounded and writes until the connection's write timeout. *Symptom:* a call a slow store used to answer in more than a second is refused with the deadline in the error's chain; the token store names `TokenStoreConfig.CallTimeout` on it. *Remedy:* size the bound with `LockerConfig.CallTimeout`, `NonceGuardConfig.CallTimeout`, `TokenStoreConfig.CallTimeout` or `ServerSentEventBackplaneConfig.CallTimeout`.
- **`amqp` backplane call timeout.** Each stretch of a server-sent event `Publish` over amqp, the wait for its turn and the write, is bounded by [`ServerSentEventBackplaneConfig.CallTimeout`](../../integrations/amqp/v4/server_sent_event_backplane.go), one second by default. *Symptom:* a broadcast a slow broker used to take in more than a second fails, and a write that outlives the timeout cuts an owned connection, which is redialed on the next publish. *Remedy:* set `CallTimeout` to the budget a broadcast may take, a `bound.Duration` in the fourth major (see the Amqp section under Unreleased).
- **`amqp` delay buckets.** The transport refuses at construction a delay bucket below one millisecond or beyond the 32-bit millisecond wire limit ([`transport.go`](../../integrations/amqp/v4/transport.go)), as it already refused a descending pair. *Symptom:* a boot declaring such a bucket panics with `amqp transport delay buckets must be between 1ms and the 32-bit millisecond wire limit`. *Remedy:* keep every bucket inside that range; a longer delay is not one the broker can hold.
- **`opentelemetry` nil middleware.** [`module.go`](../../integrations/opentelemetry/v4/module.go) refuses at boot a nil entry in `ModuleConfig.Middlewares` or `ModuleConfig.HandlerDecorators`, which it used to skip. *Symptom:* the boot panics with `opentelemetry module received a nil middleware` or `… a nil handler decorator`, typically over a constructor whose error was discarded. *Remedy:* check the constructor's error and pass only what it built.
- **`awss3` empty credential.** [`NewClient`](../../integrations/aws/s3/v4/provider.go) refuses an empty access key or secret key, which minio read as a request for anonymous access. *Symptom:* a boot whose credential variable is unset fails with `object storage credentials are incomplete`, where it used to start anonymously, serve reads off a public bucket and fail every write. *Remedy:* set both credentials; an application that means anonymous access builds the minio client itself.
- **`websocket` module configuration.** The [`websocket`](../../integrations/websocket/v4/module.go) module refuses, when it registers its route at boot, an empty `Path`, and `NewStreamHandler` refuses a nil `Hub` at the same moment; both used to register no route. *Symptom:* the boot panics naming the missing value, where it used to start with the stream endpoint answering 404. *Remedy:* configure the hub and the path, or do not register the module.
- **`websocket` empty topic.** A `TopicResolver` that answers an empty topic has the connection refused before the upgrade ([`handler.go`](../../integrations/websocket/v4/handler.go)); it used to be subscribed to a topic every such connection shared. *Symptom:* a client whose topic could not be extracted is refused instead of receiving the broadcasts of every other such client. *Remedy:* have the resolver answer a topic for every connection it accepts.
- **`websocket` keepalive.** A received pong refreshes the connection's liveness mark, and a keepalive ping that could not be written because a data frame was in flight is no longer read as a dead peer ([`handler.go`](../../integrations/websocket/v4/handler.go)). *Symptom:* a configuration with `IdleTimeout` below `WriteTimeout` no longer turns transient write contention into a disconnect — a frame in flight excuses a timed-out ping until one interval past the configured write timeout. *Remedy:* none; a receive-only client bridged onto a broadcast hub stops being disconnected for never sending a data frame.
- **`outbox` relay lease.** The distributed lease is released on a context detached from the run, the locker bounding its own release, and a release failure is logged rather than discarded ([`relay.go`](../../integrations/outbox/v4/relay.go)). *Symptom:* a graceful restart no longer stalls outbox delivery for a whole `LockTtl`. *Remedy:* none.
- **`outbox` lease lost mid-batch.** A lease refresh that fails no longer ends `Relay.RunOnce` at that row ([`relay.go`](../../integrations/outbox/v4/relay.go)): the claimed rows are fenced by their claim token and invisible to the new holder, so the batch is drained to its end under its claim tokens, the refresh is not tried again, and the failure is reported once the batch is done. *Symptom:* a relay that lost its lease used to leave every unreached row of the batch claimed and invisible to every replica for the whole `VisibilityTimeout`; it now finishes the batch, and the `melody:outbox:relay` loop logs the refresh failure and backs off after it. *Remedy:* none; a caller of `RunOnce` that matched the refresh error by identity reads it through `errors.Is` now, since it is wrapped with the lock name.
- **`outbox` panics in the codec or the transport.** A panic raised by `MessageCodec.Decode` or `Transport.Send` is contained per row ([`relay.go`](../../integrations/outbox/v4/relay.go)) and charged as the failure the collaborator would have returned — a decode panic dead-letters the row, a send panic takes the retry path — with the panic in `last_error` under a `panic: ` prefix and an `outbox delivery panicked` record in the log naming the row and the phase. *Symptom:* a relay process that died at one row on every claim, until the crash-poison cap dead-lettered it, now resolves that row and goes on. *Remedy:* none; a supervisor that relied on the crash to notice a poison row reads the log record or the `dead` rows instead.
- **`outbox` a failed send ends the batch.** [`Relay.RunOnce`](../../integrations/outbox/v4/relay.go) stops at the first failed send: that row alone is charged an attempt, unless the transport marked the failure as an outage (see the section on an unreachable transport, which charges nothing), and the rows the batch had not reached go back to pending uncharged, available again when the failed row is tried again, with a `last_error` naming the row that held them; a failed send the transport returned as an error writes an `outbox message send failed` ERROR record once the row's resolution is written, a contained send panic keeping its own record. *Symptom:* a broker outage no longer dead-letters every claimed message after about 66 minutes at the default settings; it charges the first row of each claimed batch alone, one dead-letter per batch per ~66 minutes, and every poll that finds rows enqueued during the outage claims a batch of its own; once the broker is back the held rows wait out that row's current backoff, at most `Backoff.Max`, and a message the broker refuses holds the rows behind it for its own
  backoff each time it is due. A dashboard of `last_error` sees the `held behind outbox row N` text on rows that were never tried. *Remedy:* none; a broker that refuses a message for good dead-letters it at `MaxAttempts`, as before.
- **`outbox` store claim limit.** `Store.ClaimDueMessages` ([`store.go`](../../integrations/outbox/v4/store.go)) refuses a non-positive limit by name and a limit above 100000. *Symptom:* a direct caller of the store that passed zero, a negative or a value past the int32 range used to claim the whole table in one transaction — bun writes no `LIMIT` clause for such a value; it now gets an error carrying the limit. *Remedy:* pass a positive limit no larger than 100000; the fourth major refuses the limit above it, see the Outbox section under Unreleased.
- **`opentelemetry` metrics status.** [`metrics_middleware.go`](../../integrations/opentelemetry/v4/metrics_middleware.go) records a handler error at the client-facing status the kernel derives from it — a `404`/`403` carried as an `HttpException` graphs at its own status — instead of always `500`, and [`tracing_middleware.go`](../../integrations/opentelemetry/v4/tracing_middleware.go) sets that status on the span rather than omitting it on an errored request. *Symptom:* a route whose normal contract is a 4xx no longer reads as 100% 5xx under `http.server.request.*`, and spans for handled 4xx now carry `http.response.status_code`. *Remedy:* a dashboard or alert that treated every errored request as a server error is now more precise; only a non-http error or a 5xx exception counts as `500`.
- **`bunorm` provider logger.** [`manager_registry.go`](../../integrations/bunorm/v4/manager_registry.go) hands `Provider.Open` the registry's own logger, which carries the application's logger and forwards its level threshold, instead of the application's logger itself. *Symptom:* a custom provider that type-asserted the logger it receives to the application's concrete logger type no longer matches. *Remedy:* use the logger through `loggingcontract.Logger`; a provider that needs its own logger takes it at construction rather than asserting it out of `Open`.
- **`opentelemetry` committed status.** [`metrics_middleware.go`](../../integrations/opentelemetry/v4/metrics_middleware.go) and [`tracing_middleware.go`](../../integrations/opentelemetry/v4/tracing_middleware.go) record a status the handler already committed to the writer over a response or an error it returns afterwards, and the span reads the status a handler writes directly. *Symptom:* a handler that wrote `202` and then returned a `500` response or an error graphs and traces as `202`, not `500`; the span of such a request is no longer marked in error, while the error stays recorded as its exception event; a streaming, proxy or upgrade handler's span now carries `http.response.status_code`; and a handler returning both a response and an error is traced at the error's status. *Remedy:* none needed for dashboards that read the client's status; an alert that counted a failure after a committed success as a `5xx` reads the span's exception events instead.
- **`opentelemetry` and `awss3` credential redaction.** `otlp.Config` ([`tracer_provider.go`](../../integrations/opentelemetry/v4/otlp/tracer_provider.go)) and `awss3.Config` ([`provider.go`](../../integrations/aws/s3/v4/provider.go)) gain `String` and `Format` methods that redact their credential-bearing fields — `Headers`, and `AccessKey`/`SecretKey` — on every `fmt` verb, mirroring the `encrypt` key provider. *Symptom:* a log or error context that formatted one of these configs printed the collector token or the storage keys in the clear and now prints a redacted rendering with the safe fields intact. *Remedy:* none; if a tool parsed the old raw `%v` of these structs, read the fields directly instead.
- **`websocket` shut-down hub.** [`handler.go`](../../integrations/websocket/v4/handler.go) refuses a connection against a hub that has already shut down before the upgrade, reading the new `ServerSentEventHub.IsClosed`, instead of upgrading it to a `101` and closing it instantly with only a generic "read loop ended" debug line. *Symptom:* a client connecting during the shutdown drain gets a refusal rather than an established-then-vanished stream, and the refusal is recorded. *Remedy:* none.
- **`messagebus` in-memory transport close.** `InMemoryTransport.Close` ([`transport_in_memory.go`](../messagebus/transport_in_memory.go)) now closes the delivery channel `Receive` handed out, so a consumer ranging over it sees the end of stream; it previously closed only its internal signal and left the channel open forever. *Symptom:* a userland consumer that ranged `Receive()` without its own context cancellation, and so blocked on `Close`, now ends cleanly. *Remedy:* none; sends are serialised against the close, so no send lands on a closed channel.
- **`messagebus` route transport.** `RouteType` ([`routing.go`](../messagebus/routing.go)) refuses a nil or typed-nil transport at registration instead of dereferencing it on the dispatch path. *Symptom:* a mis-wired route now fails at registration, in the framed form `RegisterTransports` uses, rather than panicking on the first message its type routes. *Remedy:* register a non-nil transport for every routed type.

### Wiring: the generator refuses what it used to drop silently

**What changed.** `melody:wiring:generate` fails, naming the site, on the inputs it used to read as "nothing": an unknown `//melody:` directive (a mistyped `scoped` demoted a request-lifetime service to a never-closed singleton; a mistyped `ignore` registered the constructor it acknowledged), a `//melody:bind` assignment without the equals sign or with an empty half (the override beside the constructor silently fell back to a broader bind), a malformed exclude pattern (`path.Match`'s `ErrBadPattern` was read as "does not match", so the exclusion excluded nothing), an empty import path or directory on a package binding (an empty directory scanned the whole project tree as one package), and two constructors that would register the same container key (the generated file panicked at first boot while the generation had reported success). `//melody:ignore` now accepts a trailing reason, which is the spelling the refusal of unknown directives makes mandatory to honour, and a directive written
with a blank after its slashes, `// melody:ignore`, is refused naming the line and the spelling it expected, where it was read as a plain comment and the constructor it meant to acknowledge was registered. An exclude that matched no constructor is reported like an unused bind, `--strict` fails on it, and a strict refusal carries every violation — binds, excludes, skipped constructors — in one error instead of the first found.

**Symptom.** A generation that used to succeed over a tree carrying any of these now fails with an error naming the file and line, and a `--strict` pipeline with a dead exclude goes red. `wiring.Scan` takes a `ScanRequest`, `ProjectDirectory`, `PackageBinding` and the `BuildTags` `--tags` names, nil for none, mirroring `GenerateRequest`, so a positional call no longer compiles, `wiring.ScanWithBuildTags`, the name the third major gave the tagged form, is removed, and a nil binding is refused by name.

**Remedy.** Correct the named site: fix the directive spelling or drop the blank after its slashes, add the equals sign, terminate the character class, split the two constructors or route one through `//melody:ignore`. Every refusal is a defect the generated file would otherwise carry into boot — none of them is a new rule about correct input.

### Wiring and openapi: the `--out` contract hardens, and the openapi anchor moves

**What changed.** Both generate commands write through a temp file and a rename, so an interrupted write leaves the previous artifact intact instead of a torn one; both refuse to replace a file that is not theirs — wiring by the `DO NOT EDIT` marker, openapi by the target not holding a JSON document; wiring refuses an `--out` inside a scanned package directory, which its own documentation always forbade; and a relative `--out` on `melody:openapi:generate` and `melody:routes:manifest` is now anchored at the project directory, exactly as the wiring command has always anchored its own — the manifest command also writes through a temp file and a rename and refuses a target that is not one valid JSON document, a file that merely opens with a brace included — with the parent directories created on the way.

**Symptom.** `melody:openapi:generate --out openapi.json` run from a working directory other than the project root — a systemd unit, a Makefile in a subdirectory — now writes into the project instead of into that directory; a pipeline that relied on the old CWD anchoring reads the file from the wrong place. A mistyped `--out` pointing at a hand-written file fails instead of destroying it.

**Remedy.** Pass an absolute `--out` to pin any other destination; delete a foreign file deliberately if its path really is the intended output. Nothing changes for the documented invocations run from the project root.

### Openapi: the document's shape becomes faithful to the router, and stable

**What changed.** A route registered with no method list — which the router answers on every verb — is documented on all eight path item verbs instead of as an operation-less `{}`; a verb the format cannot model (`PURGE`) is named in the path item's new `description` instead of vanishing; a catch-all pattern stops the documented path at the catch-all segment, because the router discards everything after it; converging routes no longer overwrite each other's operations (the route the router would serve wins: the higher priority first, then the earlier registration); and response types are visited in status order, so component names and every `$ref` to them stop depending on map iteration and the generated file is byte-stable across runs.

**Symptom.** A committed `openapi.json` regenerated after this change may differ once — method-less routes gain operations, catch-all paths shorten, colliding component names settle onto the lower status — and then stays byte-identical run over run, which is the property the diff-based pipelines were missing.

**Remedy.** Regenerate the committed document once and review the diff; it is the document moving onto what the router actually serves. Declare explicit method lists on routes that should not advertise all eight verbs.

### Configuration: the shared parsers refuse the values that used to disarm a guard

**What changed.** Three shapes that used to pass through the typed accessors are now refused. A bare integer read as a duration — `RegisterRuntime("ttl", 30)` followed by `Parameter.Duration()` — was interpreted as nanoseconds and produced a timeout that expires instantly with no error anywhere, while the same value spelled `"30"` was already refused for its missing unit; it is now refused the same way. `Float64` refuses NaN and the infinities, which `strconv.ParseFloat` parses from `"NaN"`, `"Inf"` and `"Infinity"` without complaint and which silently disarm every threshold written the normal way, since each ordered comparison against NaN is false. And a typed-nil `map[string]string` reads as absent rather than as an empty present map, so a caller branching on the presence flag to apply a default gets the default.

**Symptom.** A parameter or bag entry carrying one of the three shapes now produces an error where it used to produce a value. In every case the value it used to produce was wrong: an instant timeout, a threshold that stopped guarding, a default that never applied.

**Remedy.** Spell a duration with its unit — `"30s"` or a `time.Duration` — and give a ratio a finite value. A caller that genuinely wants an empty map for a nil one applies that default itself, which is now possible because the absence is reported.

### Configuration: `IntWithDefault` panics on a parameter that exists but does not parse

**What changed.** The helper answers the default only for a parameter that is ABSENT. A parameter that exists and does not parse as an int now panics, naming the failure, instead of silently becoming the default.

**Symptom.** A boot that used to run with a default now fails, naming the parameter whose value could not be read.

**Remedy.** Correct the value, or remove the parameter to fall back to the default deliberately. A mistyped value that quietly turns into a number nobody wrote is a misconfiguration running in disguise, and the panic is what makes it visible at boot rather than in production behaviour nobody can explain.

### Configuration: the `.env` grammar matches godotenv's, and a malformed reference fails the boot

**What changed.** A `.env` line is preprocessed byte by byte rather than through runes, so a file saved as anything other than UTF-8 keeps its bytes — a rune round-trip re-encoded them, rewriting values, a password among them, where godotenv alone passes a quoted value through untouched. The comment cut now matches godotenv's own: a `#` opening before the key separator comments the whole line out, while a `#` after it stays in the produced line once the value has begun and godotenv's countback decides where the value ends, which stops the double cut that read `hello # world # x` as `hello` where godotenv reads `hello # world`. The one comment the countback cannot reach — the `#` that opens before any value byte, `APP_SECRET= # fill this in`, which sits at index zero of the trimmed value — is cut by the preprocessor under the same space-before rule, so the value reads as empty; `KEY=#glued` stays data. And a `${...}` reference whose closing brace arrived over a name outside the key
grammar is refused rather than surviving as literal text — nobody types `${...}` into a password by accident — while an unclosed brace stays data, like the bare dollar it is.

**Symptom.** A non-empty value carrying a hash after the separator keeps more of itself than it used to, and an empty one followed by a comment reads as empty rather than as the comment; a non-UTF-8 file stops being rewritten for its quoted values, while an unquoted value is still re-encoded to U+FFFD by godotenv itself, which the fourth major refuses (see "Config: a process must be told its environment, and a per-environment file cannot rename it"); a `.env` holding a malformed `${...}` fails the boot naming the enclosing key, where it used to load the braces as text, and so does an undefined, self- or mutually-referencing `${X}`, which used to expand to an empty string.

**Remedy.** Correct the malformed reference, or escape a literal dollar as `\$`. For the comment cut and the encoding there is nothing to do: both moves take the reading onto what godotenv itself does, which is what the surrounding contract always promised.

### Configuration: an unterminated `%env(` and an unclosed `%name%` fail the boot

**What changed.** A `%env(` that never closes with `)%`, and a name-shaped run opened by a percent that nothing closes, are both refused at boot instead of surviving as literal template text. The contract already demanded that a literal percent be written doubled, so a `%app-name%` that reaches a service verbatim is a typo, not data. A percent standing in front of a character no name may start with — `growth of 50% overall` — is still data and still resolves untouched. `RegisterRuntime` additionally refuses a blank name and one carrying leading or trailing whitespace, which used to register a parameter nobody could look up under the name they wrote.

**Symptom.** A boot that used to succeed over a template holding one of these now fails, naming the parameter and the byte offset of the percent — never the name-shaped run or the unterminated tail, which are slices of the value (`Pa%SSword1` would have put `%SSword1` in the boot record); a registration with a padded name fails where it used to be accepted.

**Remedy.** Close the placeholder, double the literal percent, or trim the name. Every refusal replaces a template fragment that was being handed to a consuming service as though it were the value.

### Container: registration refuses a typed-nil provider and a colliding identity key

**What changed.** The one gate all three registration paths go through refuses a typed-nil provider function — `var f containercontract.Provider[T]` handed in uninitialized — instead of registering a signature-valid function that panics on its first call. And two DIFFERENT types whose identity keys coincide (pointer-to-unnamed-composite types drop the package path, so `*[]alpha.Bus` and `*[]beta.Bus` of same-short-named packages read identically) refuse to coexist, at the container door and the scoped one alike.

**Symptom.** A boot that used to report success and fail at the first resolution now fails at the registration line, naming the service; a wiring whose two same-keyed types used to produce false circular-dependency reports at resolution and merged nodes at teardown now refuses the second declaration at boot.

**Remedy.** Initialize the provider before registering it. For a genuine identity-key collision, name one of the composite types — a defined type alias (`type buses []contract.Bus`) carries its package path and keys uniquely.

### Container: a closed container refuses writes, and a finished teardown refuses resolutions

**What changed.** `Register` and `OverrideProtectedInstance` on a closed container refuse the way the scoped registrar always has, and a resolution after the teardown finished is refused instead of answered out of the maps. Between the two closing states the memoized instances are still served, so a service's own `Close` keeps resolving what it reports through.

**Symptom.** A late registration that used to report success for a service no resolution could ever build now returns the closed-container error; a resolver kept across shutdown that used to receive a closed service with a nil error now receives the same error.

**Remedy.** Usually nothing — both were silent failure modes. A shutdown path that legitimately resolves during the teardown (a drain reporting through the logger) is unaffected: the refusal starts only after the last `Close` returned.

### Container: an override must fit every registered type of its name, and an override installed mid-creation wins

**What changed.** `OverrideProtectedInstance` refuses, before anything is written, a value that cannot sit under a type its name is registered under — judged the way the readers will judge it, raw assignability for an interface registration and canonical identity for a value-typed one. And an override installed while a provider was still building the same service wins the slot: the creation that lost closes the value it built and the resolution answers the override.

**Symptom.** An override of the wrong type used to be accepted and then served through `GetByType` — a poisoned type cache — and now returns an error naming the registered type and the value's. A test that installed an override during a warm-up race and sometimes had it overwritten now keeps it deterministically.

**Remedy.** Fix the override's type where the refusal points. Nothing else: the race half had no correct outcome before.

### Container: the teardown closes replaced built instances and breaks ties on creation order

**What changed.** An override replacing an instance the container itself built moves the replaced instance to a graveyard the teardown closes — under the same identity marks that keep a pointer shared by several names from closing twice — instead of leaking it; the scope's `ClosedWithScope` filings evict into the same kind of graveyard. And where the dependency graph says nothing, both teardowns now close in **creation order, latest first**, instead of by the node key descending.

**Symptom.** A process whose overrides used to strand container-built values no longer leaks them at shutdown. A teardown whose order between unrelated services was decided by spelling — `app.worker` closed before the logger, `zz.worker` after — now closes what was built later first, so a service built during another's construction closes before it.

**Remedy.** Nothing for the graveyard. If a test pinned the exact close order of services with no declared dependency edge, it was pinning a string comparison; declare the edge or accept the creation order.

### Configuration: the http shutdown wait becomes a parameter, and two dead interfaces are gone

**What changed.** `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`) sets how long a stopping http server waits for the requests it has already admitted, defaulting to five seconds — the value the framework used to hardcode. It reaches the server through `config/contract.HttpConfiguration.ShutdownTimeout()`, and the same budget bounds the join of the `OnHttpShutdown` hooks and the drain of the request scopes. `HttpTimeoutConfiguration` and `HttpShutdownConfiguration` are deleted from the application package: nothing implemented either, nothing could inject one, and the branch that read them would have applied their zero values verbatim to the server had it ever become reachable.

**Symptom.** A boot with `MELODY_HTTP_SHUTDOWN_TIMEOUT=0` or a negative value now fails with `http shutdown timeout must be positive`, where before the key did not exist. A type written against either deleted interface no longer compiles.

**Remedy.** Set the key to whatever your supervisor's termination grace allows, or leave it unset for the previous five seconds. There is no replacement for the deleted interfaces: the per-request limits are fixed in this major and the shutdown wait is the parameter.

### Application: three boot doors close, and one refuses a name nothing consumes

**What changed.** An **http** process whose `.env` artifacts contributed no keys at all refuses to boot rather than serve on development defaults; a cli process stayed permissive until a defaulted `MELODY_ENV` was refused in every mode (see "Config: a process must be told its environment, and a per-environment file cannot rename it"). A module registered from inside a module boot hook is refused, and so is a route queued from inside one. `RegisterConfiguration` refuses any name but `logging`.

**Symptom.** A production binary started from a directory without its `.env` files, which used to serve on `dev` with the profiler and the debug commands enabled behind a single warning, now fails the boot naming the project directory. A composition root that registered a module or a route from inside `RegisterServices` or any other hook — where it never took effect — now sees a refusal. A configuration registered under a misspelled name, which used to be stored and never read, now fails the boot.

**Remedy.** Give the http process its environment files, with `MELODY_ENV` set in one of them (a key set in the process environment is ignored, since configuration is read from the artifacts alone, and named in a boot warning when it matches a resolved parameter and its value differs from the resolved one). Move module registrations to `ModuleProvider` and route registrations to `RegisterHttpRoutes`, the hooks made for them. Correct the configuration name to `logging`.

### Application: the framework's default services can be substituted, and a middleware factory may not yield nil

**What changed.** The logger, the url generator, the serializer manager, the default serializer and the validator are registered only when the container does not already hold them, the way the cache, the session and the firewall manager already were; `service.serializer`, which two documented resolvers named and nothing registered, is now registered. The logger is additionally resolved once eagerly at boot, so a provider that cannot build fails the boot instead of the first request. A middleware factory returning nil, or a typed nil, fails the pipeline build naming the definition. The firewall manager is registered whatever the process mode.

**Symptom.** An application that used to hit the duplicate-registration exit when bringing its own serializer manager now substitutes it. A logging configuration that cannot build now fails at the boot step that owns it rather than at an arbitrary later resolution. A middleware factory that returned nil to disable itself conditionally now fails the boot.

**Remedy.** A factory that wants to disable itself returns a pass-through middleware instead of nil. Everything else needs no action.

### Application: the boot order puts the application's routes first and the kernel listeners last

**What changed.** `bootHttp` runs before the module phases, so the application's own routes register before any module's and win the router's registration-order tie-break; the duplicate-route refusals are collected into the aggregated boot report rather than panicking one at a time. The kernel's default listeners are wired at the end of `Boot` in every process shape, so a console process's dispatcher answers with the same set the serving process runs, the http kernel's built-in exception listener apart, which `debug:events` declares.

**Symptom.** Where an application route and a module route shared a pattern, the application's now wins where the module's used to. A boot with several duplicate routes reports them together instead of stopping at the first. `debug:events` in a console process no longer reports an empty dispatcher.

**Remedy.** Nothing, unless a deployment relied on a module route shadowing an application route on the same pattern — declare the intended one and remove the other.

### Httpclient: `buildUrl` is RFC 3986 reference resolution, and a base url wants its trailing slash

**What changed.** The target is resolved against the base url by RFC 3986 reference resolution — the rule Symfony and Guzzle implement — instead of being appended to the base as a prefix: an absolute-path target (`/users`) replaces the base path entirely, a relative one (`users`) merges over the last segment of the base path, and an empty target names the base resource itself, with its trailing slash. Because the merge cuts the last segment of a base spelled without a trailing slash, `NewHttpClient` refuses such a base; a base with an empty path (`https://host`) stays legal.

**Symptom.** `NewHttpClient(httpclient.HttpClientConfig{BaseUrl: "https://host/v1"})` answers the error `the base url path must end with a slash`. A client rebuilt with `"https://host/v1/"` and calling `Get("/users", ...)` reaches `https://host/users` where the old join reached `https://host/v1/users`.

**Remedy.** Spell the base with its trailing slash and the targets relative: `"https://host/v1/"` + `"users"` names `https://host/v1/users` under both rules. Targets that begin with `/` are the ones whose meaning changed — drop the leading slash to keep them under the base path. The refusal at construction is deliberate: without it the changed meaning would surface as a 404 in production.

### Httpclient: the foreign-origin refusal reads the resolved url, and a relative target needs a base url

**What changed.** On a client WITH a base url, the refusal of a target leaving the base origin is judged on the RESOLVED url rather than on the target's spelling, so the network-path form (`//other.example/x`) and any case of the scheme are covered. On a client WITHOUT a base url, a relative target is refused by name instead of failing inside `net/http` with the cause unnamed.

**Symptom.** `Get("//other.example/x")` on a based client answers `the request url leaves the origin of the configured base url` where it used to hang the reference under the base path. `Get("/users")` on a base-less client answers `the request url is relative and the client has no base url` instead of `failed to create request`.

**Remedy.** A caller that talks to more than one origin builds a client without a base url, as before. A caller that hit the second refusal was already failing — the error now names the missing half.

### Httpclient: header maps are canonicalized at every door

**What changed.** `NewHttpClient` refuses a configuration whose `Headers` carry two spellings that collapse onto one header, naming the collision; a request whose `RequestOptions.Headers` carry such a map fails before anything is dialled. `HttpClient.SetHeader` stores the canonical spelling. The client and the request copy the maps they are given.

**Symptom.** A configuration map holding both `x-api-key` and `X-Api-Key` is refused naming the collision; a request built with such a map fails with `header keys collide after canonicalization` instead of being sent. A header rotation through `SetHeader("x-api-key", ...)` on a client configured with `X-Api-Key` overwrites the entry it means to instead of leaving two. A write into a map after the call no longer reaches the client or the request.

**Remedy.** Keep one spelling per header in each map, and build the map before the call.

### Session: an injected file handle opened for appending is refused

**What changed.** `session.NewFileStorage` over a `File` refuses a handle opened with `O_APPEND`, naming it, and the write itself now names offset zero instead of seeking to it.

**Symptom.** A construction that used to succeed now fails at boot with `session storage file is opened for appending` — or, for a handle opened read-only, with `session storage file is opened read-only`.

**Remedy.** Open the handle without `os.O_APPEND` — `os.O_RDWR|os.O_CREATE` is what this storage needs — or hand the path as `session.NewFileStorage(session.FileStorageConfig{Path: p})` and let it own the file. The refusal replaces a silent corruption: an appending write ignores the offset, so every snapshot landed after the document it was replacing and the truncation then cut the pair to the new length. Measured, a growing snapshot left the file readable and lost every save with no error on any path, and a shrinking one left a document the next boot refused to decode, losing every persisted session.

### Session: a userland `Storage` may now be called concurrently for different sessions

**What changed.** `Manager.SaveSession` and `Manager.DeleteSession` hold a lock striped by the session id (256 stripes, so two ids that hash to one stripe still serialise) across the storage call instead of one lock for the whole manager. The tombstone check and the write are still one critical section for the SAME session, which is the invariant that stops a logout being undone by a request that loaded the session before it; two requests acting on different sessions no longer wait on each other. Sixteen concurrent saves of distinct sessions against a storage with a 2 ms round trip took 35.5 ms and now take 2.8 ms.

**Symptom.** An implementation of `session/contract.Storage` written against the previous behaviour — one that assumed the manager serialised every call and therefore keeps unsynchronised state of its own — can now be entered concurrently for different session ids. Both storages melody ships took a mutex of their own all along and are unaffected; so is any storage that delegates to a client which is already safe for concurrent use, which redis and database clients are.

**Remedy.** Make the implementation safe for concurrent calls naming different session ids — for most storages this is already true and needs no change. Calls naming the same id are still serialised by the manager, so per-session state needs nothing added.

### Http: the end of a session is a warning, not a storage outage

**What changed.** When another request ends a session while this one is running, the response path records it at **warning** under its own name. The two genuine failures on that path — a save that could not land and a delete that could not land — keep the error level. All of them now carry a one-way reference to the session id (`sessionReference`, never the id itself), the request method and the path.

**Symptom.** `session was deleted while the request was in flight` moves from `error` to `warning`. An alert counting session errors will see its volume drop, by exactly the traffic that was never a failure: a user logging out in one tab produced one of these per concurrent request in the others.

**Remedy.** If an alert was tuned around that volume, retune it; the records it was counting were the session ending, which `SESSION.md` and `HTTP.md` both describe as the normal outcome.

### Http: a handler failure is one record, at the level it deserves

**What changed.** A failure a handler returns is filed once — a deliberate 4xx and a client's own cancellation at warning, everything else at error — and the record is marked, so the exception listener attaches its request coordinates to it instead of filing the same failure again.

**Symptom.** A journal consumer that counted two error records per handler failure counts one, and alerting keyed on error-level records no longer fires for a routine 404 or a 429 the rate limiter refused.

**Remedy.** Re-base incident counts on the single record and route the warning level to whatever used to read the error one for refusals; nothing in the application changes.

### Session: an atomic Snapshot is `Session.Snapshot`

**What changed.** `session/contract.Session` declares `Snapshot() (values map[string]any, modified bool, cleared bool)` — the three answers read under one lock acquisition — and the framework's session implements it. The response path and `SaveSession` read a session through `session.Snapshot`, which calls it. Reading the flags and the values through the individual accessors let a `Clear` racing the response land between the reads and write the pre-logout state back under a live id. The sibling `SnapshotSession` the third major asserted, and its reading through `All`, `IsModified` and `IsCleared`, are removed.

**Symptom.** An out-of-tree implementation of the `Session` contract no longer compiles until it declares `Snapshot`.

**Remedy.** Implement `Snapshot` as one critical section over the same three answers the accessors give; a single-threaded implementation can simply return `instance.All(), instance.IsModified(), instance.IsCleared()`.

### Session: the manager no longer closes a storage it was handed

**What changed.** `session.NewManager` builds a manager that does not own its storage: `Close` leaves the storage open, because a storage handed in was built by someone else and is closed by whoever built it — on the container path both are registered services and the container closes each one itself, so the cascade closed the storage twice, which a storage wrapping a connection typically reports as a failure on the second call and turns a clean shutdown into a reported one. `ManagerConfig.OwnsStorage` keeps the cascade for the caller that builds both by hand. This is the same ownership rule the cache manager took, and it is stated here because the session manager took it too.

**Symptom.** A hand-wired `NewManager` caller that relied on `manager.Close()` to close its storage finds the storage still open: a file handle is not released, and a storage that runs a goroutine keeps running it for the life of the process.

**Remedy.** Build with `session.NewManager(storage, session.ManagerConfig{OwnsStorage: true})` if the manager is meant to own the storage, or close the storage where it was built. On the container path there is nothing to do — the container already closes both.

### Session: a sub-second positive ttl is refused by the manual constructor too

**What changed.** `session.NewManager` refuses a `ManagerConfig.Ttl` below one second, the refusal `MELODY_HTTP_SESSION_TTL` validation gives too: such a lifetime stores no usable session — `SaveSession` reports success and the entry lapses before the client returns.

**Symptom.** A hand-wired manager built with, say, `500*time.Millisecond` now panics at construction naming the field; zero is refused the same way, and no expiry is `bound.UnlimitedDuration()`.

**Remedy.** Use `bound.Of(ttl)` with a lifetime of at least one second, or `bound.UnlimitedDuration()` for no expiry.

### Behavioural: `Session.Clear` latches, and a deleted session cannot be saved again

**What changed.** Two related refusals. [`Session.Clear`](../session/session.go) now latches: a later `Set` puts the value back and marks the session modified, but the session stays cleared, so the response path still deletes it. And [`Manager.DeleteSession`](../session/manager.go) remembers the id for `ManagerConfig.TombstoneRetention`, five minutes by default, so [`SaveSession`](../session/manager.go) refuses a write under an id another request deleted, returning an error whose cause is [`ErrSessionDeleted`](../session/manager.go). [`RegenerateSession`](../session/manager.go) refuses such an id the same way, before anything is stored, with `ErrSessionDeleted` and, for an id an earlier rotation buried, [`ErrSessionRotated`](../session/manager.go) beside it. The unexported `abandon`, which applied the latch for rotation alone, is gone.

**Symptom.** A handler that clears a session and then writes to the same object no longer keeps the session alive: previously the write lifted the cleared flag and the response path saved the session back under the pre-logout id and re-issued its cookie. And a request holding a session that another request deleted mid-flight now gets an error from `SaveSession` instead of silently re-creating the entry; the response path answers that by expiring the browser cookie and serving the handler's response unchanged.

**Remedy.** A handler that wants a usable session after ending one asks the manager for a new session rather than writing to the cleared one:

```go
sessionInstance.Clear()

replacement, mintErr := manager.NewSession(runtimeInstance)
if nil != mintErr {
    return nil, mintErr
}

replacement.Set(sessionKeyFlash, "you have been signed out")

if publishErr := melodyhttp.SetRequestSession(request, replacement); nil != publishErr {
    return nil, publishErr
}
```

Code that calls `SaveSession` directly should treat `ErrSessionDeleted` as the session having ended rather than as a failure:

```go
if err := manager.SaveSession(runtimeInstance, sessionInstance); nil != err {
    if true == errors.Is(err, session.ErrSessionDeleted) {
        /* another request signed this session out; nothing to persist */
        return nil
    }

    return err
}
```

### Behavioural: a negative session ttl fails at construction

**What changed.** [`session.NewManager`](../session/manager.go) panics on a negative `ManagerConfig.Ttl`, as the configuration path already did.

**Symptom.** Code that computed a ttl dynamically and could produce a negative value — `time.Until(expiry)` on an instant already past — now fails at construction instead of building a manager. It previously produced sessions with **no expiry at all**, because both storages test `0 < ttl` and treat anything else as "never expires": the value that reads as "already lapsed" produced the immortal session.

**Remedy.** Clamp before constructing, and use `bound.UnlimitedDuration()` when no expiry is what you mean:

```go
ttl := time.Until(expiry)
if time.Second > ttl {
    ttl = time.Second
}

manager := session.NewManager(storage, session.ManagerConfig{Ttl: bound.Of(ttl)})
```

### Behavioural: `Session.Get` hands out a copy

**What changed.** [`session.Session.Get`](../session/session.go) returns a copy at the depth `All` copies at. The live nested value it used to hand out, mutated in place, changed the session without passing through `Set`: `modified` stayed false, `SaveSession` skipped the write and reported success, and the mutation silently never persisted.

**Symptom.** Code that mutated the map or slice returned by `Get` and relied on the live session object changing underneath — within the same request only, since the mutation never reached the storage — now works on its own copy.

**Remedy.** Read, mutate the copy, `Set` it back — the pattern that was always the correct one is unaffected:

```go
profile := sessionInstance.Get("profile").(map[string]any)
profile["role"] = "admin"
sessionInstance.Set("profile", profile)
```

### Behavioural: `ManagerRegistry.Close` waits for the opens still in flight

**What changed.** [`ManagerRegistry.Close`](../../integrations/bunorm/v4/manager_registry.go) publishes its refusal and tears the memoized pools down as before, and then WAITS for the opens — ordinary and migration alike — that were in flight when the refusal was published.

**Symptom.** `Close` can now block for as long as a dial started before it takes to finish. It never leaked before either: such an open ends its own freshly opened database against the closed flag. What changes is the answer's meaning — `Close` returning now means the teardown is over, where it used to mean the teardown of everything already memoized was over while a dial was still in the air, and a process exiting on that answer left the connection outstanding for a server-side timeout to reap.

**Remedy.** Nothing to build: the registry's close signal, fired by its `CloseWithContext`, ends the context of every open in flight, and so does the caller's own context, handed through its runtime; the cancellation reaches the open in flight — its refusal before the attempt, its configuration hook, its boot ping, its retry sleep — so the wait is as short as the provider can make it. Registered as a service, the registry is closed through `CloseWithContext` under the teardown budget, which bounds the wait; a direct teardown calls `CloseWithContext` itself, `context.Background()` cancelling the open in flight as well and leaving the wait that follows without a deadline, which is the case the teardown budget exists for.

### Behavioural: a sub-millisecond retry backoff is refused, and a constant multiplier costs no square

**What changed.** Both dialect providers ([`mysql`](../../integrations/bunorm/mysql/v4/provider.go), [`pgsql`](../../integrations/bunorm/pgsql/v4/provider.go)) refuse an initial delay or a ceiling below one millisecond, and compute the growth in closed form instead of walking it once per attempt already made.

**Symptom.** A `RetryConfig` asking for a sub-millisecond `Backoff.Initial` or `Backoff.Max` is refused: `NewProvider` panics naming `RetryConfig.Backoff.Initial` or `RetryConfig.Backoff.Max`. The guards only ever refused a non-positive value, so one nanosecond was the smallest thing that could be configured — a wait shorter than the dial it separates, which is the re-dial storm those guards exist to prevent, reached through the door they left open. Separately, a `Backoff.Factor` of exactly 1 — a valid constant backoff — never left the growth walk early, so a large `MaxAttempts` paid O (attempt²) for a delay that never moved.

**Remedy.** None for any configuration that was backing off at all: the delays a sane configuration produces are unchanged, and the closed form answers the same values the walk did. A configuration that asked for a sub-millisecond delay writes at least `bound.Of(time.Millisecond)` — this package will not produce a shorter wait.

### Translation: the catalogs of one locale are asked in order

**What changed.** [`NewManager`](../translation/manager.go) keeps every catalog of a locale, in the order given, and a lookup asks them in that order until one answers. It used to key the locale on one catalog, so a second catalog of the same locale replaced the first. A catalog whose `Locale()` is empty is refused at construction, as a nil catalog is: the locale chain never asks for the empty locale, so it could never be found.

**Symptom.** An application that handed `NewManager` two catalogs of one locale — one built by the json loader and one by hand, say — gets the messages of both, the earlier catalog answering a message both hold; it used to get the later catalog alone, every message that lived only in the earlier one answering its raw id. An application that passed a catalog with an empty locale panics at construction where it used to boot with a catalog nothing could reach.

**Remedy.** None for the common wiring. An application that relied on the later catalog overriding the earlier one passes the overriding catalog first.

### Security: a raw prefix rule refuses `PUBLIC_ACCESS`

**What changed.** `accesscontrol.NewRawPrefixRule` refuses `PUBLIC_ACCESS` with a panic at construction. A raw public prefix opens every path that merely starts with it (`/public` opens `/publicadmin`) and, being the longest match, shadows a bounded denial beneath it. The third major's `security.NewAccessControlRule`, which built through it, is removed with the other deprecated rule constructors.

**Symptom.** A boot that declares `accesscontrol.NewRawPrefixRule("/public", accesscontrol.RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})` panics with `access control PUBLIC_ACCESS may not be declared on a raw prefix rule; use a segment prefix, exact, or regex rule`. A rule without `PUBLIC_ACCESS` is unchanged by this section; the path refusals of the raw prefix rule (a relative, a non-canonical or a slash-terminated path) are those of the access-control section above.

**Remedy.** Declare the public area with `AccessControlBuilder.AllowAnonymous("/public")` or `accesscontrol.NewSegmentPrefixRule`. Each governs `/public` and `/public/...` and nothing else.

### Security: a regex rule carrying `PUBLIC_ACCESS` must anchor every branch

**What changed.** The guard asks the parsed pattern whether every branch is anchored to the path start, instead of testing the text for a leading `^`.

**Symptom.** `^/public|/status` now panics at boot, because its second branch matched anywhere. `(?m)^/public` is refused as well. `(?i)^/public(/|$)` and `\A/public`, which the textual test refused, are accepted.

**Remedy.** Anchor each branch, as in `^(/public|/status)(/|$)`.

### Security: `NewHmacTokenSource` refuses a `MaxFutureExpiry` below its minimum

**What changed.** The third major's v3.14.0 refused a negative `MaxFutureExpiry` at construction, which had behaved like zero, an unbounded horizon. In the fourth major the field is a `bound.Duration` of 1h when left out, and a value under a second or unlimited is refused at construction, naming the field.

**Symptom.** A configuration carrying a negative, zero or sub-second value fails when the token source is built.

**Remedy.** Leave the field out for the one-hour horizon, or set the bound you meant, at least a second.

### Security: `totp.Config.Period` above one day is refused

**What changed.** A period past 24 hours is refused naming `totp.Config.Period`, as an out-of-range skew is. A period of `1<<63` or more used to freeze the counter.

**Symptom.** `totp.Verify`, `GenerateCodeAt` and `OtpauthUri` answer a period longer than a day as their error, and the second-factor authenticator panics at construction.

**Remedy.** Keep the period at or below 24 hours, in whole seconds. Authenticator apps use 30.

### Security: a firewall prefix with a trailing slash claims the bare path

**What changed.** `PathPrefixMatcher` registered on `/admin/` also matches `/admin`, which the router serves as the same route.

**Symptom.** A request for `/admin` reaches the firewall registered on `/admin/`, where it used to pass unguarded.

**Remedy.** None, unless `/admin` was meant to be public. In that case, give it its own exact rule.

### Http: the static file server refuses dot-prefixed paths

**What changed.** A path element beginning with a dot is refused, so a `.env` or `.git/config` left in the public directory is no longer served. `http/static.DefaultAllowedDotPrefix` (`.well-known`) is admitted, and `static.Config.AllowedDotPrefixList` names the others.

**Symptom.** The file server no longer serves a dot-prefixed file outside `.well-known/`. The request passes on as if the file were missing, and a warning `static serve dot prefixed path element` is logged.

**Remedy.** Add each prefix that is meant to be public to `AllowedDotPrefixList`, `.well-known` with them.

### Http: a request whose `RawPath` no longer matches `Path` is refused

**What changed.** The kernel refuses with a 400, before authorization, a request whose `URL.RawPath` no longer unescapes to `URL.Path`. This is the state a handler mounted in front of the kernel leaves when it rewrites `Path` alone.

**Symptom.** Behind a front handler that strips a prefix from `URL.Path` only, every request carrying an escape answers 400, and a warning carries `rawPath` beside `path`.

**Remedy.** Rewrite both fields in the front handler, as `nethttp.StripPrefix` does, and never clear `RawPath`.

### Http: a session-cookie response is kept out of shared caches

**What changed.** A response carrying the session cookie drops `public` from `Cache-Control` and adds `private`, unless it already says `private` or `no-store`. The expiring cookie of a cleared or deleted session is handled the same way.

**Symptom.** A shared cache or CDN that stored such responses stops storing them.

**Remedy.** None. A response meant for a shared cache carries no session cookie.

### Http: the default rate-limit key is the client IP alone, and the key map is bounded

**What changed.** The default key extractor keys on the client IP instead of `ip:path`. Both limiters track at most `LimiterConfig.MaxKeys` distinct keys, 1,000,000 by default. Once the map is full, the key least recently seen is evicted and the request under the unseen key admitted.

**Symptom.** A client that stayed under the limit on each path separately is now limited across all of them.

**Remedy.** Raise the limit to the per-client budget you meant, or, if the per-path allowance was intended, set `RateLimitConfig.KeyExtractor` to one that returns the client IP joined with the path.

### Security: an access-control rule whose attributes normalize to empty is refused

**What changed.** A rule whose attribute list is empty after trimming is refused at construction. It used to be accepted as a rule that granted every authenticated principal and shadowed any denial with a longer prefix.

**Symptom.** A boot declaring such a rule panics with `access control rule requires at least one attribute`.

**Remedy.** Give the rule its attributes, or drop it.

### Security: a refusal without a response is answered fail-closed

**What changed.** When the access-control or resolution path refuses a request and no `kernel.exception` listener produced a response, a generic fail-closed response is written. The kernel used to read the nil response as "no decision" and serve the handler.

**Symptom.** A request that used to reach its handler despite a refusal now answers with the refusal.

**Remedy.** None. If a custom entry point produced no response, it now falls through to the fail-closed 401. Have it return the response it means.

### Http: a middleware may reference only a middleware active wherever it is

**What changed.** A `before` or `after` reference is refused when the referenced middleware is not enabled in every environment the referring one is enabled in. The declared environment sets are compared, not the booted environment.

**Symptom.** A pipeline that booted in the environment at hand now fails at boot. It would have failed in another environment.

**Remedy.** Enable the referenced middleware in every environment of the referrer, or narrow the referrer's environments.

### Cache: a zero `waitTimeout` takes a memoized result without waiting

**What changed.** A `waitTimeout` of zero means no waiting, not no answer. A result the in-flight call has already memoized is taken without blocking, and only a flight still in the air answers with the timeout.

**Symptom.** A `Remember` with a zero wait that always answered the timeout now answers the value when one is ready.

**Remedy.** None.

### Bag: a key present with a nil value reads as unset from the string readers

**What changed.** `String`, `StringSlice`, `StringStrict` and `StringSliceStrict` answer `exists == false` for a key present with a nil value. `v3.13.0`'s `BAG.md` documented `("", true)` for it. The typed readers already answered `false`.

**Symptom.** Code that treated `exists == true` with an empty string as "sent empty" now takes its missing-key branch for a nil value.

**Remedy.** Test `Has` for the presence of the key.

### Configuration: `Resolve()` is refused once the application serves

**What changed.** `Configuration.Resolve()` answers an error after `Run` has begun serving. It used to re-resolve under readers that had already settled.

**Symptom.** A call to `Resolve()` from a handler or a background goroutine returns `cannot resolve the configuration once the application has begun serving`.

**Remedy.** Register and resolve parameters during boot.

### Openapi: `Describe` is refused once the application serves

**What changed.** `Application.Run` marks the openapi registry serving at the moment it marks the configuration, and a `Describe` issued after it is refused, naming the route.

**Symptom.** A route described from a handler or a goroutine started after boot now panics.

**Remedy.** Describe routes during boot, beside their registration.

### Application: a relative `MELODY_LOG_PATH` is anchored at the project directory

**What changed.** A relative `MELODY_LOG_PATH` is anchored at the project directory, the rule the logs and cache directories already follow, and the file's parent directory is created before the open.

**Symptom.** A process started from a working directory other than the project root writes its journal into the project, not into that directory.

**Remedy.** Use an absolute `MELODY_LOG_PATH` to pin another location.

### Logging: every process reopens its journal on `SIGHUP`

**What changed.** The container-built logger opens its file journal through `NewReopenableFileWriter` and reopens it on `SIGHUP`, the signal logrotate sends, in every process: the http process and the console commands, a long-running worker included.

**Symptom.** `SIGHUP` no longer terminates the process; it reopens the journal. A console worker that relied on `SIGHUP` to stop, or a command whose terminal hung up, keeps running.

**Remedy.** Stop the process with `SIGTERM` or `SIGINT`. Point logrotate's `postrotate` at `SIGHUP` instead of `copytruncate`.

## v3.0.0

v3 is a separate import path, so an application moves onto it by rewriting its imports rather than by resolving a new version. The entry below is the one rewrite that does not compile afterwards: v1 and v2 keep the identifiers, v3 has never carried them.

### Compile-level: `validation` does not carry the twelve deprecated constants

**What changed.** [`validation/const.go`](../validation/const.go) declares `ServiceValidator`, `ErrorInvalidRuleSyntax`, `ErrorUnknownRule` and `ErrorNestingDepthExceeded` and nothing else. The twelve deprecated aliases that v1 and v2 still declare are absent. Each one was defined as the constant that replaces it, so every replacement carries the identical string and the rewrite is a rename:

| Absent in v3           | Replacement                                                                              | Value                |
|------------------------|------------------------------------------------------------------------------------------|----------------------|
| `ErrorNotAlpha`        | [`ConstraintAlphaErrorNotAlpha`](../validation/constraint_alpha.go)                      | `notAlpha`           |
| `ErrorNotAlphanumeric` | [`ConstraintAlphanumericErrorNotAlphanumeric`](../validation/constraint_alphanumeric.go) | `notAlphanumeric`    |
| `ErrorInvalidEmail`    | [`ConstraintEmailErrorInvalidEmail`](../validation/constraint_email.go)                  | `invalidEmail`       |
| `ConstraintMax`        | [`ConstraintMaxLength`](../validation/constraint_max_length.go)                          | `max`                |
| `ErrorMaxLength`       | [`ConstraintMaxLengthErrorTooLong`](../validation/constraint_max_length.go)              | `tooLong`            |
| `ConstraintMin`        | [`ConstraintMinLength`](../validation/constraint_min_length.go)                          | `min`                |
| `ErrorMinLength`       | [`ConstraintMinLengthErrorInsufficientLength`](../validation/constraint_min_length.go)   | `insufficientLength` |
| `ErrorNotBlank`        | [`ConstraintNotBlankErrorIsBlank`](../validation/constraint_not_blank.go)                | `isBlank`            |
| `ErrorEmpty`           | [`ConstraintNotEmptyErrorEmpty`](../validation/constraint_not_empty.go)                  | `empty`              |
| `ErrorNotNumeric`      | [`ConstraintNumericErrorNotNumeric`](../validation/constraint_numeric.go)                | `notNumeric`         |
| `ErrorRegexMismatch`   | [`ConstraintRegexErrorMismatch`](../validation/constraint_regex.go)                      | `regexMismatch`      |
| `ErrorInvalidPattern`  | [`ConstraintRegexErrorInvalidPattern`](../validation/constraint_regex.go)                | `invalidPattern`     |

`ConstraintMax` and `ConstraintMin` are rule names — the token a `validate` tag spells — and the other ten are error codes a client reads off a validation failure.

**Symptom.** Code that named any of them stops compiling with `undefined: validation.ErrorNotAlpha` and the like. The failure is per identifier, so a package that used several reports several.

**Remedy.** Rename each reference to the replacement column above:

```go
/* v1 / v2 */
if validation.ErrorNotBlank == validationError.Code() {

/* v3 */
if validation.ConstraintNotBlankErrorIsBlank == validationError.Code() {
```

Nothing outside the Go source changes: the strings are identical, so a `validate` tag, an api client matching on the error code, and a translation catalogue keyed on it all keep working untouched.
