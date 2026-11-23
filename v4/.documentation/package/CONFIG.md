# CONFIG

The [`config`](../../config) package provides `.env` loading and strongly typed configuration for Melody. It builds a [`configcontract.Configuration`](../../config/contract/configuration.go) from `.env` files and exposes typed sub-configurations (kernel, HTTP, CLI) used by the application, kernel, runtime, and HTTP stack.

## Scope

Configuration in Melody is file-driven (via `.env` artifacts). The `config` package does not overlay OS environment variables by design: the application is a black box that behaves identically wherever it runs, and its configuration travels with its artifacts.

The project directory — where the `.env` artifacts are looked up — is derived from the **executable location**; only under `go run` (a `go-build` temp path) does it fall back — to the working directory when it holds an `.env` artifact, else to the nearest ancestor holding a `go.mod`, and to the working directory only when none does. The consequence: `go run .` finds your `.env`, but the same app built with `go build -o /tmp/app && /tmp/app` from the same directory does not — it looks next to `/tmp/app`. Deploy the `.env` artifacts next to the binary. Boot makes this visible: a warning names the searched directory when zero environment keys were loaded, and the resolve failure carries the `projectDirectory` in its context.

The corollary is easy to miss in deployment: **setting a process environment variable does nothing**. `MELODY_DEFAULT_MODE`, `MELODY_PROCESS_ROLE`, `MELODY_HTTP_ADDRESS` and every other parameter must live in the `.env` files; an `environment:` entry in docker-compose is silently inert. To make this foot-gun visible, the application writes the [boot warning](SECURITY.md#boot-warnings) `application.environment.processVariableIgnored` for every process environment variable named like a `.env` key: a declared key, compared with its parameter's resolved value, a key the artifacts loaded and nobody declared, compared with its line, or a [retired key](#retired-keys) (see `application/environment_warning.go`) — the warning is suppressed when the process value equals the compared one, never for a retired name, which it names with its `successor`, and values are never logged. For per-container variation from a single image, use the runtime flags instead (`--mode`, `--role`).

A `Configuration` instance is created early (typically at application bootstrap) and is then passed into the kernel and other framework components.

## Subpackages

- [`config/contract`](../../config/contract)
  Public contracts for configuration and sub-configurations (`Configuration`, `KernelConfiguration`, `HttpConfiguration`, `CliConfiguration`, `EnvironmentSource`, `Parameter`).

## Responsibilities

- Load `.env` values from a filesystem via [`EnvironmentSource`](../../config/environment_source.go)
- Construct a typed configuration via [`NewConfiguration`](../../config/configuration.go)
- Resolve templated parameters via [`(*Configuration).Resolve()`](../../config/configuration_resolve.go)
- Provide container access helpers:
    - [`ServiceConfig`](../../config/service_resolver.go)

## Configuration

### `.env` loading order

`EnvironmentSource` reads values from the base directory in this order:

1. `.env`
2. `.env.local`
3. `.env.<env>`
4. `.env.<env>.local`

A `#` opens a comment when it is preceded by a space byte or by nothing: a whole line, or the tail of a value once the value has begun (`KEY=value # note` reads `value`, by godotenv's own countback), or the whole value region when nothing precedes it (`KEY= # fill this in` reads the empty string); a hash glued to what precedes it is data (`COLOR=#ffffff`, `KEY=#glued`), as it is for a shell.

A quoted value is kept byte for byte, whatever its encoding: the file is read as bytes and never decoded, so a password saved as Latin-1 between quotes reaches its parameter with exactly the bytes the file holds. An unquoted value that is not valid UTF-8 is refused at boot, naming the key and the file and never the value: quote it. The value is judged as godotenv reads it: a comment after it is not judged, and a second statement on its line is judged as its own value.

The environment name is resolved from `MELODY_ENV` inside the loaded `.env` values (the `.env.<name>` files it selects default to `dev` when it is missing or empty). See [`EnvironmentNameKey`](../../config/environment.go) and [`EnvironmentSource`](../../config/environment_source.go). A process whose `MELODY_ENV` no artifact sets fails the boot, in every mode, naming the key: the default selects the `.env.dev` files and turns the debug tooling on, so a production process would run on the development files without a word. A process environment variable does not set it; a command run before any `.env` exists, in a build stage for instance, writes one first (`printf 'MELODY_ENV=prod\n' > .env`). An http process whose `.env` artifacts contributed no key at all is refused for that first.

**A present-but-empty `MELODY_ENV` selects the `.env.dev` files and then fails the boot.** The default above applies to the *file selection* — an empty value picks `dev` and `.env.dev` / `.env.dev.local` are loaded — but the parameter itself keeps the empty value it was given, and the kernel configuration refuses it: `environment may not be empty`, naming `MELODY_ENV`, `kernel.environment`, and the files the emptiness already selected. The two readers deliberately disagree, because an empty value is an accident of a rendered deployment template rather than a request to run in development, and the alternative — booting on the development files because a variable came out blank — is the failure mode worth refusing. Give it a value: a removed key is refused too, as a defaulted name, unless `.env.dev` sets it.

**The `.env.<name>` files cannot rename the environment that selected them.** The name is read once, from `.env` and `.env.local`, to choose the files; once the four files are merged and the references expanded, `MELODY_ENV` must still name that environment, or the load fails naming the key, both names and the two per-environment files. A per-environment file may repeat the name; an absent or empty value reads as `dev` on both sides, so a `.env.dev` naming `prod` under a defaulted name is refused too. Symfony's Dotenv admits the rename; melody refuses it, since it runs the production files as `dev` with the debug tooling on.

### Empty string semantics

A present key with an empty string value is considered **present** (it is a valid value for string parameters). Typed conversions for non-string getters treat empty strings as invalid, by design.

### Environment variable keys

All recognised `.env` keys and their defaults live in [`config/environment.go`](../../config/environment.go) and [`config/configuration_alias.go`](../../config/configuration_alias.go). The table holds the framework's keys; a module declares its own (see [Module keys](#module-keys)) and its README lists them.

Keys and parameters are two tables. A key is not a parameter: `Get("MELODY_HTTP_ADDRESS")` answers nil, and a template reads a key only as `%env(KEY)%`. The parameter a key feeds holds the template `%env(KEY)%`, so `kernel.http_address` answers the `.env` line, or the declared default when no artifact sets the key. A `%KEY%` reference to a key is refused at boot with `a template references an environment key as a parameter; a key is read as %env(KEY)%`, naming the parameter, the key and the offset. A line nobody declared stays readable through `%env(KEY)%`, whatever its spelling, a dotted one included. Only a key of upper-case letters, digits and underscores can be declared, though, so rename a lower-case line (`database_url`) before declaring it:

| Env key                                   | Parameter name                            | Default                                            |
|-------------------------------------------|-------------------------------------------|----------------------------------------------------|
| `MELODY_ENV`                              | `kernel.environment`                      | none: a boot without it is refused                 |
| `MELODY_DEFAULT_MODE`                     | `kernel.default_mode`                     | `"http"`                                           |
| `MELODY_PROCESS_ROLE`                     | `kernel.process_role`                     | `"all"`                                            |
| `MELODY_HTTP_ADDRESS`                     | `kernel.http_address`                     | `":8080"`                                          |
| `MELODY_HTTP_MAX_REQUEST_BODY_BYTES`      | `kernel.http.max_request_body_bytes`      | `1048576`                                          |
| `MELODY_HTTP_SESSION_TTL`                 | `kernel.http.session_ttl`                 | `24h`                                              |
| `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | `kernel.http.session_tombstone_retention` | `5m`                                               |
| `MELODY_HTTP_SESSION_STORAGE_TIMEOUT`     | `kernel.http.session_storage_timeout`     | `5s`                                               |
| `MELODY_HTTP_SESSION_MAX_SESSIONS`        | `kernel.http.session_max_sessions`        | `100000`                                           |
| `MELODY_HTTP_SHUTDOWN_TIMEOUT`            | `kernel.http.shutdown_timeout`            | `""` (the application's, then `5s`)                |
| `MELODY_TEARDOWN_TIMEOUT`                 | `kernel.teardown_timeout`                 | `""` (the application's, then `10s`)               |
| `MELODY_CONTAINER_CREATION_WAITERS`       | `kernel.container_creation_waiters`       | `""` (the application's, then `1024`)              |
| `MELODY_CONTAINER_TEARDOWN_SEQUENTIAL`    | `kernel.container_teardown_sequential`    | `""` (the application's, then `false`)             |
| `MELODY_HTTP_PUBLIC_URL`                  | `kernel.http.public_url`                  | `""` (the application's, then none)                |
| `MELODY_HTTP_READ_TIMEOUT`                | `kernel.http.read_timeout`                | `""` (the application's, then `15s`)               |
| `MELODY_HTTP_READ_HEADER_TIMEOUT`         | `kernel.http.read_header_timeout`         | `""` (the application's, then `5s`)                |
| `MELODY_HTTP_WRITE_TIMEOUT`               | `kernel.http.write_timeout`               | `""` (the application's, then `30s`)               |
| `MELODY_HTTP_IDLE_TIMEOUT`                | `kernel.http.idle_timeout`                | `""` (the application's, then `60s`)               |
| `MELODY_HTTP_MAX_HEADER_BYTES`            | `kernel.http.max_header_bytes`            | `""` (the application's, then `1048576`)           |
| `MELODY_HTTP_UNAVAILABLE_RETRY_AFTER`     | `kernel.http.unavailable_retry_after`     | `""` (the application's, then `5s`)                |
| `MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE`     | `kernel.http.session_empty_on_outage`     | `""` (the application's, then `false`)             |
| `MELODY_HTTP_SESSION_COOKIE_SECURE`       | `kernel.http.session_cookie_secure`       | `""` (the application's, then `from-scheme`)       |
| `MELODY_CLI_NAME`                         | `kernel.cli_name`                         | `"melody"`                                         |
| `MELODY_CLI_DESCRIPTION`                  | `kernel.cli_description`                  | `""`                                               |
| `MELODY_CLI_BOOT_WARNINGS_IGNORED`        | `kernel.cli_boot_warnings_ignored`        | `""`                                               |
| `MELODY_CLI_BOOT_WARNINGS_DISABLED`       | `kernel.cli_boot_warnings_disabled`       | `""` (`false`)                                     |
| `MELODY_LOG_PATH`                         | `kernel.log_path`                         | `%kernel.logs_directory%/%kernel.environment%.log` |
| `MELODY_LOG_LEVEL`                        | `kernel.log_level`                        | `"debug"`                                          |
| `MELODY_DEFAULT_LOCALE`                   | `kernel.default_locale`                   | `"en"`                                             |
| `MELODY_PUBLIC_DIRECTORY`                 | `kernel.public_directory`                 | `"public"` (set explicitly, it must exist)         |
| `MELODY_STATIC_INDEX_FILE`                | `kernel.static.index_file`                | `"index.html"`                                     |
| `MELODY_STATIC_CACHE_DISABLED`            | `kernel.static.cache_disabled`            | `false`                                            |
| `MELODY_STATIC_CACHE_MAX_AGE`             | `kernel.static.cache_max_age`             | `3600`                                             |
| `MELODY_STATIC_EXCLUDED_PATHS`            | `kernel.static.excluded_paths`            | `""`                                               |

Project layout defaults that are not env-overridable:

| Parameter name             | Default                                |
|----------------------------|----------------------------------------|
| `kernel.project_directory` | set from the application bootstrap     |
| `kernel.logs_directory`    | `%kernel.project_directory%/var/log`   |
| `kernel.cache_directory`   | `%kernel.project_directory%/var/cache` |

### Limit keys

Every key that holds a limit or a timeout is read as a [`bound`](../../bound/limit.go) limit: the empty value is absent and takes the default, `unlimited` lifts the bound where the field admits it, and anything else is a value — a duration with its unit (`30s`, `24h`) or a count — judged against the field's minimum. A value below the minimum, an `unlimited` the field refuses and a text that is none of the three fail the boot with `invalid environment value`, naming the key, the cause naming the field. The minimums: the body cap 1 byte, the cache max age 0, the session ttl and the tombstone retention 1s, the shutdown timeout, the teardown budget, the scope teardown timeout and the four server timeouts 1ms, the header cap 4 KiB, the container creation waiters and the session count 1. Only the session ttl, the session count, the teardown budget, the scope teardown timeout and the container creation waiters admit `unlimited`.

The keys marked "the application's" are absent unless the deployment sets them — the eight limit keys here, the teardown switch described under [Container teardown order](#container-teardown-order) and the session switch described under [Session storage outage](#session-storage-outage): the application's [`ApplicationConfig`](../../application/application_new.go) field is the default of the `kernel.*` parameter the key feeds, so the key wins over it unless a [parameter override](#parameter-overrides) rewrites the parameter (see the application document's [Server limits](APPLICATION.md#server-limits)). [`HttpConfiguration`](../../config/contract/http.go) answers those five server keys, and [`KernelConfiguration.TeardownTimeout()`](../../config/contract/kernel.go), [`ScopeTeardownTimeout()`](../../config/contract/kernel.go) and [`ContainerCreationWaiters()`](../../config/contract/kernel.go) the teardown, the scope teardown and the creation waiters keys, as the deployment set them; the other limits it answers resolved, a [`bound.Resolved`](../../bound/resolve.go) holding the value or `Unlimited`.

### Module keys

A module owns its `.env` keys through [`EnvironmentKeyModule`](../../application/contract/environment_key_module.go): first in `Boot`, before any parameter hook, it hands the framework one [`EnvironmentKey`](../../config/contract/environment_key.go) per key — the key, the parameter its code reads, the field a refusal names, the default, the secret mark and the rule. The parameter reads the key, as `kernel.http_address` reads `MELODY_HTTP_ADDRESS`, so `%melody.cron.user%` and `%env(MELODY_CRON_USER)%` resolve to the same value: the `.env` line when a file sets the key, the declared default otherwise, as for a framework key. A module key is resolved when it is declared, so the module reads it at once; a default that references a parameter registered later, or a key another module declares later, waits for the boot resolve. An application's key is resolved when the configuration is built, before any module declares, so its line or default reading a module key through `%env(...)%` is refused there; a line nobody declared is resolved only when a template reads it.

- **Precedence.** The `.env` line wins, then the default the module declared; a module that takes the value as a config field too passes that field as the default, so the key wins over the field, which wins over the rule's default.
- **Rules.** [`LimitKeyRule`](../../config/environment_key_rule.go) reads a limit as the [Limit keys](#limit-keys) above are read, [`SwitchKeyRule`](../../config/environment_key_rule.go) a boolean, [`ChoiceKeyRule`](../../config/environment_key_rule.go) one of a list compared exactly. Under any of them a present, empty line is absent and takes the declared default; a free-text key keeps the empty value. The module reads a limit through [`ResolveLimitParameter`](../../config/environment_key_rule.go) and a boolean through [`ParameterSwitch`](../../config/environment_key_rule.go), the parsers the boot check runs.
- **Refusals at declaration.** The boot fails naming the owner, the key and the field for a key that is not `MELODY_` followed by upper-case letters, digits and underscores, a key the framework or another declaration holds or [retired](#retired-keys), a retired spelling the [Retired keys](#retired-keys) section refuses, a parameter name that is empty, padded, in `kernel.` or the key itself, a parameter another key feeds or the application already registered, a secret key with a default and a literal default its own rule refuses. A parameter hook that registers a declared parameter by hand is the duplicate of the aggregated boot report.
- **Refusal of the value.** After the resolve and the framework's own refusals, every declared key is judged on its resolved value, in every mode and role in which the module is registered: `invalid environment value` naming `environmentKey`, `owner`, `field` and `parameterName`, the rule's error in the chain, withheld for a secret key.
- **Inactive modules.** A module the process does not register declares nothing: its key is a line nobody declared, readable through `%env(KEY)%`, with no default and no rule.

### Application keys

An application declares the `.env` keys it owns in [`ApplicationConfig.EnvironmentKeys`](../../application/application_new.go), one [`EnvironmentKey`](../../config/contract/environment_key.go) each, with the default, the rule, the secret mark and the retired spellings of a module's key, and every refusal of one. They are declared when the application is built, in slice order, after the framework's keys and before any parameter is read, so a default may read a key declared before it, and every module declares after them. Their owner is `the application`, named under `owner` by a refusal and by `invalid environment value`, which judges them after the resolve with the modules' keys. An application's key is any name of upper-case letters, digits and underscores starting with a letter outside the `MELODY_` namespace, which stays the framework's and the modules', so a process that does not register a module still warns about and redacts its keys; an application's key in the namespace is refused with `an application's environment key is in the framework's MELODY_ namespace`. Since the application's keys are declared before the modules', a default of one that reads a module's key through `%env(...)%` takes that key's `.env` line and is refused at construction when no artifact sets it, as a `.env` line is; a module's default is not read there. A module may not be named `the framework` or `the application`. Outside the application, the keys reach the configuration through [`ConfigurationConfig.EnvironmentKeys`](../../config/configuration.go).

The `MELODY_` namespace belongs to the framework and the modules. Every loaded `MELODY_*` key that is neither the framework's nor declared is named, sorted, in one boot warning `application.environment.undeclaredKey`, and every parameter reading it through `%env(KEY)%` is marked secret at the boot resolve, since it may be misspelt or belong to a module this process does not register.

### Parameter overrides

[`ApplicationConfig.ParameterOverrides`](../../application/application_new.go) rewrites, by parameter name, the template of a framework (`kernel.*`) parameter or of a parameter a module's key feeds: `"%env(APP_HTTP_ADDRESS)%"`, `"%app.param%"`, a literal, or any mix. The override is the parameter's one source: the key that fed it is no longer read, and [`Parameter.IsOverridden()`](../../config/contract/parameter.go) answers true, `IsDefault()` false, `EnvironmentKey()` still the key and `Template()` the override. The key's own name keeps reading the key. Outside the application, the overrides reach the configuration through [`ConfigurationConfig.ParameterOverrides`](../../config/configuration.go).

- **When it applies.** An override of a `kernel.*` parameter is applied when the configuration is built, after the application's keys are declared and before the views are built, so the limits, the container, the runtime flags and the static server read it; it is resolved there, so it reads keys, the application's declared defaults included, literals and other `kernel.*` parameters, and a value computed in Go is written as the literal. An override of a module's parameter waits for the module to declare the key that feeds it and is resolved with it, a reference to a parameter registered later waiting for the boot resolve.
- **Refusals at construction.** An empty or padded name; `kernel.environment`, which selected the `.env.<name>` files before the configuration existed; `kernel.project_directory`, the anchor the artifacts were read from; a `kernel.*` name that is no parameter; the parameter of one of the application's own keys, whose template is the key's `Default`; a `kernel.*` override that cannot be resolved, `failed to resolve parameter` naming it with `overridden: true`. A value its rule refuses fails as a set key's would, `invalid environment value` naming the key, the parameter under `parameterName` and `overridden: true`, its cause withheld when the parameter is secret, since it quotes the value.
- **Refusals of a module parameter's override.** A text that cannot be read at all, an unterminated `%env(` for one, refuses the module's declaration with `a declared environment key could not be resolved at its declaration` and `overridden: true`, leaving the tables as they were; a reference to a parameter or key not yet defined waits for the boot resolve, which refuses it with `failed to resolve parameter` and `overridden: true`. An override of a module's parameter in a process that does not register the module is not consumed, so it refuses the boot as below: such an override belongs to the process that registers the module.
- **Refusals at boot.** After the modules declare, an override no framework or module key consumed refuses with `a parameter override names a parameter no framework or module key feeds`, the names under `parameterNames`. After the [retired keys](#retired-keys), a `.env` artifact that still sets the key of an overridden parameter refuses, unless the override reads that key: `the environment key <key> is set and reads nothing: the application overrides <parameter>; remove the line`, every such key in one refusal joined with `; ` in key order, the keys, parameters and owners under `key`, `parameter` and `owner` and never a value. Presence refuses, an empty line included.
- **Secrets.** An overridden parameter is secret when its template reads a secret key or parameter, and when the key that feeds it is declared `Secret`: the mark says what the parameter carries, whatever its source. A mark on the key's value alone, a `${…}` reference to a secret key, does not reach an override that does not read it.
- **Cli boot warnings.** An override of `kernel.cli_boot_warnings_ignored` or `kernel.cli_boot_warnings_disabled` is code the cli process reads; an http process writes every boot warning and ignores it. Only a `.env` line of either key refuses an http boot, as [Cli boot warnings](#cli-boot-warnings) says.

### Retired keys

A key that was renamed refuses the boot naming its successor, whatever its value, an empty one included, and with the successor set beside it too:

| Retired key                  | Successor                      | Owner           |
|------------------------------|--------------------------------|-----------------|
| `MELODY_PUBLIC_DIR`          | `MELODY_PUBLIC_DIRECTORY`      | the framework   |
| `MELODY_STATIC_ENABLE_CACHE` | `MELODY_STATIC_CACHE_DISABLED` | the framework   |
| `MELODY_CRON_LOGS_DIR`       | `MELODY_CRON_LOGS_DIRECTORY`   | the cron module |

`MELODY_CRON_LOGS_DIR` is a spelling of the unreleased fourth major, retired before its first release; `MELODY_PUBLIC_DIR` and `MELODY_STATIC_ENABLE_CACHE` are the third major's keys. The successor of `MELODY_STATIC_ENABLE_CACHE` reads its value the other way, so its refusal adds `, and the value inverts: MELODY_STATIC_ENABLE_CACHE=false is MELODY_STATIC_CACHE_DISABLED=true`, under `note` in the context.

`Boot` judges the `.env` artifacts the process loads, embedded or on disk: `.env`, `.env.local` and the selected environment's two files, so a retired key in another environment's files refuses only the boot that selects it. It runs right after the modules declare their keys and before any other module hook, in every process shape, and names every retired key it finds in one refusal, `the environment key <key> is retired: rename it to <successor>`, joined with `; ` in key order; the refusal names keys, successors and owners, under `key`, `successor` and `owner`, and never a value, since a retired key may hold a credential. A process variable of a retired name is not configuration and is not read; the `application.environment.processVariableIgnored` boot warning names it with its `successor`, whatever its value. There is no door: the key's meaning is certain, nothing reads it, and booting on its silent default is the misconfiguration; the remedy is a one-line rename.

A module or the application retires a spelling of its own through `EnvironmentKey.Retired`. A module's retired spelling outside the `MELODY_` namespace, the application's that is not upper-case letters, digits and underscores starting with a letter, a retired spelling equal to the key or to its parameter, listed twice, held or retired by the framework or another declaration refuses the declaration, and so does a key the framework or another declaration retired; a module colliding with a key of its own is told so, with no other owner named. `RetiredEnvironmentKeySuccessor` answers the successor of any retired spelling, set or not. The application may retire a `MELODY_` spelling, so a deployment is told to rename a key of the framework's era to the application's own. The retired key of a module the process does not register stays the `application.environment.undeclaredKey` warning: the framework cannot know a rename nobody declared.

### Environment / mode / role constants

- [`EnvironmentDevelopment` (`"dev"`) / `EnvironmentProduction` (`"prod"`)](../../config/environment.go)
- [`ModeHttp` (`"http"`) / `ModeCli` (`"cli"`)](../../config/environment.go)
- [`RoleWeb` (`"web"`) / `RoleWorker` (`"worker"`) / `RoleAll` (`"all"`)](../../config/environment.go)

### Template resolution

A parameter value is a template resolved left to right, positionally. Each percent opens exactly one of:

- `%%` — one literal percent;
- `%env(KEY)%` — an environment key: the artifact's line when one sets the key, a blank line under a declared key's rule counting as unset, else the default the framework, a module or the application declared for it, itself a template; a key nobody declared and no artifact sets fails the boot, so a credential parameter refuses to start rather than degrade to empty;
- `%parameter%` — another parameter, never a key, whose fully resolved value is spliced in as data (a percent inside it survives literally, it is never rescanned);
- anything else — a lone percent is data.

A self-reference — direct or through any chain of parameters and environment keys — is a circular-reference error at resolution. A value that must hold a literal percent doubles it: `pa%%ss%%word` resolves to `pa%ss%word`. `%env(default:…)%` is gone and is refused as a malformed placeholder: a key's fallback is the `Default` of its declaration. A `.env` value's `${KEY}` is expanded when the artifacts load, before any declaration exists, so it reads a line and never a declared default; a value that needs the default writes `%env(KEY)%`.

The grammar fails closed on what would otherwise survive as literal text: an `%env(...)%` whose closing `)%` is malformed or missing (`%env(A))%`, a dsn whose final percent was forgotten), and a `%name` reference a percent opened and nothing closed (`%app-name%`), are boot errors naming the parameter; the unterminated `%env(` and the unterminated `%name` run carry the byte offset of the percent — and, when the percent sits inside an environment value read through `%env(KEY)%`, the environment key, since that value is scanned under the reading parameter's name and the offset indexes the value, not the reader's template — the name-shaped run and the unterminated tail are slices of the value and are never carried, while the bounded malformed `%env(...)%` candidate (`%env(A))%`) is refused with the placeholder echoed when it is spelled in key-grammar characters, and no offset. A percent in front of a character no name may start with stays data, so `50% overall` needs no escaping. In `.env`
values, a braced `${...}` reference whose name is outside the key grammar (`${DB-PASS}`) is refused the same way, while the bare dollar stays data (`pa$sword`, `$1.50`); a literal dollar is written `\$`.

### Secret parameters

A parameter may be declared as holding a credential so that the commands rendering the configuration redact it — the value reaching the services is untouched, this governs display only. Declare one through the module registrar with [`RegisterSecretParameter`](../../application/contract/parameter_module.go), mark a registered one by its name with [`MarkParameterSecret`](../../application/contract/parameter_module.go), or declare the key that feeds it `Secret`. The marking travels with the value: a parameter whose template reads a secret (a dsn assembling a password, through `%parameter%` or `%env(KEY)%`) becomes secret itself. [`Parameter.IsSecret`](../../config/parameter.go) reports the marking; `debug:parameters` renders a marked parameter as `********` (`(empty)` when its value is empty, so an unset secret is told apart from a set one).

melody does not guess which keys hold a credential: a parameter is not marked by its name. Declare each credential key `Secret` in `ApplicationConfig.EnvironmentKeys`. `MarkParameterSecret` takes a parameter name; a key name matches nothing and ends as the boot warning `application.secretMark.unmatched`. A parameter whose template reads a marked one is marked with it, so marking the password covers the dsn assembled from it, and a mark on the parameter a declared key feeds reaches every reader of that key.

A key declared `Secret` marks the key and its parameter with no call of the application's, and takes no default, since a default credential is shared by every deployment that forgot the line; a `MELODY_*` key nobody declared is marked too (see [Module keys](#module-keys)).

### Session ttl

How long a stored session stays valid. Read through [`HttpConfiguration.SessionTtl()`](../../config/contract/http.go).

| Environment key           | Parameter name            | Default |
|---------------------------|---------------------------|---------|
| `MELODY_HTTP_SESSION_TTL` | `kernel.http.session_ttl` | `24h`   |

The value is a Go duration string, so it **must carry a unit** — `30m`, `24h`, `168h`. An environment value always arrives as a string, so a unit-less number such as `1800` is not "1800 seconds" but a parse failure that **fails the boot** with `invalid environment value`. A value below one second fails the boot the same way, `0` included: below a second the value does not describe a short session but a broken one — the entry lapses before the client comes back with the cookie, and a lapsed entry is dropped — by the file storage on a later write, by the in-memory one on a load or a sweep — so the save reports success and the next request finds nothing. A deployment that wants no expiry writes `unlimited`.

The default is [`DefaultSessionTtl`](../../config/http.go), **24 hours**: long enough that no ordinary browsing session is cut short and short enough that abandoned entries leave. It is bounded because of what no expiry costs here specifically, and the cost does not come from anything the application wrote. Melody mints a session for every request that arrives without a session cookie and reads the session, so the moment an application writes to a session on a public path — a csrf token, a flash message, a locale — every cookie-less request leaves an entry behind: a crawler, a health check, a hotlinked image. With no expiry nothing reclaims it:

- [`InMemoryStorage`](../../session/in_memory_storage.go), the default store, only ever deletes entries that carry an expiry — its background cleanup skips entries with none — so the map fills to its count, `MELODY_HTTP_SESSION_MAX_SESSIONS`, and then refuses every new session for the process's whole lifetime;
- [`FileStorage`](../../session/file_storage.go) holds every session in one map and re-serialises **the whole map** on every write, so each save gets progressively more expensive as the file grows. Its flush does purge the entries that have lapsed, but with no expiry there are none to purge, so nothing ever leaves.

Which is why the application says so at boot: an http application that is still on the framework-supplied [`InMemoryStorage`](../../session/in_memory_storage.go) **and** on an `unlimited` ttl writes the boot warning `http.session.unboundedTtlOnBoundedDefaultStorage`, naming the count that holds it, and one that also sets `MELODY_HTTP_SESSION_MAX_SESSIONS=unlimited` is refused at boot, since nothing would ever remove an entry. Either half alone is silent — a shared storage is the operator's to prune, and a bounded lifetime reclaims on its own.

The ttl is a **lifetime since the last write, not an idle timeout.** The expiry is stamped when the session is stored, and a session is only stored when it was modified — [`Manager.SaveSession`](../../session/manager.go) returns early otherwise, and the response path calls it under the same condition — so reading a session never refreshes it. With `MELODY_HTTP_SESSION_TTL=30m` a user who logs in and then browses read-only pages is logged out 30 minutes after the login, however active they were; this is not `gc_maxlifetime`. The choice is deliberate: refreshing on read would turn every request carrying a session into a storage write, which on [`FileStorage`](../../session/file_storage.go) re-serialises the whole map. An application that wants a true idle timeout can buy one explicitly with a `kernel.request` listener that writes to the session — a last-seen timestamp, say — accepting the one storage write per request that costs.

On the default this reads: a session ends 24 hours after its last write, measured from the login when nothing writes to it after, not from the last page.

### Session tombstone retention

How long a deleted session id is remembered, so that a slow in-flight request still holding a snapshot loaded before the delete cannot write the deleted session back. Read through [`HttpConfiguration.SessionTombstoneRetention()`](../../config/contract/http.go).

| Environment key                           | Parameter name                            | Default |
|-------------------------------------------|-------------------------------------------|---------|
| `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | `kernel.http.session_tombstone_retention` | `5m`    |

The value is a Go duration string of at least one second, and `unlimited` is refused: a shorter window, zero included, fails the boot because a window that refuses nothing disarms the logout defence. The default is [`DefaultSessionTombstoneRetention`](../../config/http.go), five minutes.

### Session storage timeout

How long one call of the session manager to its storage may take: the load of the request's session and the mint after a miss, the rotation, and the save or the delete at the end of the request. The load, the mint and the rotation run on the request's own context, so a client that left stops them; the save and the delete run detached from the client's hangup, so a logout is recorded after the client went away; a delete the bound cut is followed by one read under the same bound, which decides the burial. `MELODY_HTTP_SESSION_PERSISTENCE_TIMEOUT`, its earlier spelling, is retired: an artifact that still sets it refuses the boot naming this key. Read through [`HttpConfiguration.SessionStorageTimeout()`](../../config/contract/http.go) and handed to the framework-wired manager as `ManagerConfig.StorageTimeout`, which the http kernel asks for through `Manager.StorageTimeout`.

| Environment key                           | Parameter name                            | Default |
|-------------------------------------------|-------------------------------------------|---------|
| `MELODY_HTTP_SESSION_STORAGE_TIMEOUT`     | `kernel.http.session_storage_timeout`     | `5s`    |

The value is a Go duration string of at least one millisecond, or `unlimited` for no bound, which leaves a request held for as long as the storage hangs; zero and a negative value fail the boot. A call the bound cuts is an outage: a load, a rotation or a save answers `503` with `Retry-After`, a logout's delete answers `503` with `Retry-After` and keeps the cookie, so the retry names the session. The default is [`DefaultSessionStorageTimeout`](../../config/http.go), five seconds. The bound is cooperative: a storage that ignores its context is not interrupted (see [SESSION.md](SESSION.md#contexts-and-deadlines)).

### Session max sessions

How many sessions the framework-wired [`InMemoryStorage`](../../session/in_memory_storage.go) holds at once. Read through [`HttpConfiguration.SessionMaxSessions()`](../../config/contract/http.go) and handed to the storage as `InMemoryStorageConfig.MaxSessions`; a storage the application registers under `session.storage` declares its own.

| Environment key                    | Parameter name                     | Default  |
|------------------------------------|------------------------------------|----------|
| `MELODY_HTTP_SESSION_MAX_SESSIONS` | `kernel.http.session_max_sessions` | `100000` |

The value is a count of at least one, and `unlimited` is admitted. At the count, once the lapsed sessions are swept, a new session id is refused and a stored one is still saved. The default is [`DefaultSessionMaxSessions`](../../config/http.go), 100,000. `unlimited` beside `MELODY_HTTP_SESSION_TTL=unlimited` is refused at the boot of an http process on the default storage, see [Session ttl](#session-ttl).

### Http shutdown timeout

How long a stopping http server waits for the requests it has already admitted before cutting them. Read through [`HttpConfiguration.ShutdownTimeout()`](../../config/contract/http.go), absent when the key is unset, and resolved by the application over `ApplicationConfig.HttpServer.ShutdownTimeout`, which is the key's default.

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_HTTP_SHUTDOWN_TIMEOUT` | `kernel.http.shutdown_timeout` | `""`    |

The value is a Go duration string of at least 1ms, and `unlimited` is refused: zero and negative values fail the boot. Without the key and the field the wait is [`DefaultHttpShutdownTimeout`](../../config/http.go), five seconds; a refused value names `ApplicationConfig.HttpServer.ShutdownTimeout`, and the key when the key carried it. The same budget bounds the shutdown of the server and the drain of the request scopes the kernel still holds, so a deployment whose supervisor grants a longer termination grace raises this to match.

### Teardown timeout

How long a process shutting down cleanly may spend releasing what it holds — closing pools, transports, tracer providers — before the teardown is abandoned and the process exits non-zero. Read through [`KernelConfiguration.TeardownTimeout()`](../../config/contract/kernel.go) as the deployment set it, and decided by the application against its own [`ApplicationConfig.TeardownTimeout`](../../application/application_new.go): the key wins over the field, and the field over the default.

| Environment key           | Parameter name            | Default |
|---------------------------|---------------------------|---------|
| `MELODY_TEARDOWN_TIMEOUT` | `kernel.teardown_timeout` | `""`    |

The value is a Go duration string of at least 1ms, or `unlimited`. Left out, the key is absent: the application's `ApplicationConfig.TeardownTimeout` applies, and without one [`DefaultTeardownTimeout`](../../config/kernel.go), ten seconds, which is what the commonest supervisor grants before it escalates to `SIGKILL` — raising the default past that would trade an abandoned teardown for a killed one. A value below 1ms fails the boot, `0` included.

The budget is spent in two halves. The first is the deadline handed to the services being released, and every component the container closes that can take one is given it — pools, transports, backplanes, tracer providers — so what they spend is bounded by the figure declared here rather than by a constant each of them invented. The second is the headroom in which a service that honoured its deadline is allowed to say what it could not release: a service told to finish at the very instant the shutdown gives up is abandoned every time, and its report reaches nobody. So the deadline the services see is half of what is set here, and the whole of it is what a supervisor's termination grace is measured against.

The deadline is shared, not handed out afresh to each service: it bounds the teardown as a whole, so a component that spends it starves the ones the teardown graph puts after it. That is deliberate, and it is what makes the answer readable — the journal names both the service that ate the budget and the ones that were left with none: beside the failures when a close failed, and as a warning carrying the teardown's deadline record when the teardown overran and failed nothing (see [CONTAINER.md](CONTAINER.md)).

What setting it buys is measurable. On the example application, with an amqp broker that has stopped reading: at the default the process exits non-zero in about five seconds and the journal names both the transport that failed and the tracer provider that was left no time to flush; before the budget reached the services, the same shutdown was abandoned at ten seconds with a single line on standard error naming nothing, and the named diagnosis needed a budget of ten minutes to appear at all.

**`unlimited` means no deadline**: the teardown is waited out for as long as its slowest component needs, and nothing is abandoned. That holds for the clean return; when the process exits on a recovered panic, an unlimited budget is left out of the exit handler's `ExitConfig.BeforeExitBudget`, which then takes its default of ten seconds, since that path must still reach `os.Exit`. On the same measurement it finishes in about thirty seconds with every service closed and the failure named. It is the honest answer for a deployment whose supervisor grants an open-ended stop, and only for that one — melody cannot see what grace it is running under, so under a supervisor that escalates to `SIGKILL` first, `unlimited` means the process is killed before it can report anything at all, which is strictly less than the default would have given.

The http half of a clean stop is budgeted separately, by `MELODY_HTTP_SHUTDOWN_TIMEOUT`: the server drains its in-flight requests before `Run` returns, and only then does this budget start. A supervisor's grace has to cover both, so the figure to compare against a ten-second termination grace is the sum of the two, not this one alone.

### Scope teardown timeout

How long the close of a scope gives its scoped services: the scope of a request, of a command, of a consumed message and of a cron run. Their `CloseWithContext` receives a context that ends at this term, so a service waiting on a remote gives up and the close reports it, where the close had no term at all.

| Environment key                 | Parameter name                  | Default |
|---------------------------------|---------------------------------|---------|
| `MELODY_SCOPE_TEARDOWN_TIMEOUT` | `kernel.scope_teardown_timeout` | `""`    |

The value is a Go duration string of at least 1ms, or `unlimited` for no term. Left out, the key is absent: the application's `ApplicationConfig.ScopeTeardownTimeout` applies, and without one [`container.DefaultScopeTeardownTimeout`](../../container/container.go), three seconds. A value outside that fails the boot naming the key.

The term is cooperative, as every teardown budget is: a service that ignores its context is not interrupted. On the http path the close runs before the handler returns, so a response under the server's 4 KiB buffer reaches the client only after it, and a service that honours its context holds that response no longer than this term. An http process refuses to boot unless it stays strictly under `MELODY_HTTP_SHUTDOWN_TIMEOUT`, naming both keys, since a request scope still closing when the shutdown's wait ends is reported as left open and the process exits non-zero. This holds for the default too, so a shutdown timeout of 3s or less needs a shorter scope teardown timeout beside it, and `unlimited` is for a process that serves no http.

### Container creation waiters

How many resolutions may wait on one service the container is building for another goroutine — every concurrent request of a server reaching the same singleton at a cold start — before one more is refused with [`container.ErrCreationWaitersExceeded`](../../container/errors.go). Read through [`KernelConfiguration.ContainerCreationWaiters()`](../../config/contract/kernel.go) as the deployment set it, and decided by the application against its own [`ApplicationConfig.ContainerCreationWaiters`](../../application/application_new.go): the key wins over the field, and the field over the default.

| Environment key                     | Parameter name                      | Default |
|-------------------------------------|-------------------------------------|---------|
| `MELODY_CONTAINER_CREATION_WAITERS` | `kernel.container_creation_waiters` | `""`    |

The value is a whole number of at least 1, or `unlimited`. Left out, the key is absent: the application's `ApplicationConfig.ContainerCreationWaiters` applies, and without one [`container.DefaultCreationWaiters`](../../container/container.go), 1,024. A value below 1 fails the boot, `0` included.

### Container teardown order

Whether the container closes its services one after the other instead of in dependency waves (see [CONTAINER.md](CONTAINER.md)), for a deployment whose services touch shared external state the container cannot see. Read through [`KernelConfiguration.ContainerTeardownSequential()`](../../config/contract/kernel.go) as a [`Switch`](../../config/contract/kernel.go) — set or not, and its value — and decided by the application against its own [`ApplicationConfig.ContainerTeardownSequential`](../../application/application_new.go): the key wins over the field.

| Environment key                        | Parameter name                         | Default |
|----------------------------------------|----------------------------------------|---------|
| `MELODY_CONTAINER_TEARDOWN_SEQUENTIAL` | `kernel.container_teardown_sequential` | `""`    |

The value is a boolean in any spelling the parameter conversion accepts — `true`, `false`, `1`, `0`, `yes`, `no`, `y`, `n`, `on`, `off`, in any case — and anything else fails the boot with `invalid environment value`, naming the key. Left out, the key is unset and the field decides; without the field the teardown runs in waves.

### Session storage outage

Whether a request that reads the session while the session storage cannot answer is served an empty session marked unavailable, saved nowhere and with no cookie written, where the default answers its reader the marked error, `503` with `Retry-After` when a handler returns it (see [SESSION.md](SESSION.md)). Read through [`HttpConfiguration.SessionEmptyOnOutage()`](../../config/contract/http.go) as a [`Switch`](../../config/contract/kernel.go) and decided by the application against its own [`ApplicationConfig.HttpKernel.SessionPolicy.EmptyOnOutage`](../../application/application_new.go): the key wins over the field.

| Environment key                       | Parameter name                        | Default |
|---------------------------------------|---------------------------------------|---------|
| `MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE` | `kernel.http.session_empty_on_outage` | `""`    |

The value is a boolean in the spellings the container teardown order accepts, and anything else fails the boot with `invalid environment value`, naming the key. Left out, the key is unset and the field decides; without the field the outage is answered as an error. The firewall's sign-in and sign-out doors refuse the empty session `503` either way.

### Session cookie secure

Whether the session cookie is marked `Secure` on every response whatever the application's field says. Read through [`HttpConfiguration.SessionCookieSecure()`](../../config/contract/http.go), empty when the key is unset, and decided by the application against its own [`ApplicationConfig.HttpKernel.SessionCookiePolicy.Secure`](../../application/application_new.go): the key only hardens the field.

| Environment key                     | Parameter name                      | Default |
|-------------------------------------|-------------------------------------|---------|
| `MELODY_HTTP_SESSION_COOKIE_SECURE` | `kernel.http.session_cookie_secure` | `""`    |

The value is [`HttpSessionCookieSecureAlways`](../../config/environment.go) (`always`), spelled exactly; anything else, `from-scheme` and `never` included, fails the boot with `invalid environment value`, naming the key and the admitted value. The key wins over the field, so a lower spelling would let a deployment weaken an `always` the composition root chose: `SessionCookieSecureFromScheme` and `SessionCookieSecureNever` are written in Go alone. Left out, the key is unset and the field decides; without the field the scheme decides.

### Static file cache

Whether a static response carries cache headers, and for how long. Read through [`Http().StaticCacheDisabled()`](../../config/http.go) and [`Http().StaticCacheMaxAge()`](../../config/http.go).

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_STATIC_CACHE_DISABLED` | `kernel.static.cache_disabled` | `false` |
| `MELODY_STATIC_CACHE_MAX_AGE`  | `kernel.static.cache_max_age`  | `3600`  |

The max age is a plain count of seconds. A negative value and `unlimited` fail the boot, and `0` means what it reads like: with the cache enabled, `MELODY_STATIC_CACHE_MAX_AGE=0` ships `Cache-Control: public, max-age=0` — always revalidate, with the `ETag`/`Last-Modified` machinery intact. [`NewFileServer`](../../http/static/file_server.go) refuses a negative or unlimited `CacheMaxAge` the same way, and takes the `3600` default only for a max age left out.

The way to stop handing clients that hour is `MELODY_STATIC_CACHE_DISABLED=true`, which is not the same thing: the static response then carries no `Cache-Control`, no `ETag` and no `Last-Modified` at all, so there is no validator for a conditional request to present and the file server never answers `304 Not Modified`. With no headers to go on, a shared cache falls back to its own heuristics rather than to a revalidation you asked for. See [HTTP](HTTP.md) for what the entity tag is derived from and when it fails to change. `MELODY_STATIC_CACHE_DISABLED=true` wins over a valid max age set beside it — one outside its rule still refuses the boot, since the value is judged before the switch is read — so a `.env.dev.local` can switch the cache off over a `.env` that sets one: the max age a `.env` artifact sets, a blank line or `3600` included, is ignored and named in the `http.static.cacheMaxAgeIgnored` boot warning an http process writes, each value under where it came from, the key of a `.env` line under `environmentKeys` and the parameter of an override under `parameterNames`; the default `3600` no line sets says nothing.

### Static excluded paths

Which path prefixes the built-in file server declines before it looks at the disk. Read through [`HttpConfiguration.StaticExcludedPaths()`](../../config/contract/http.go).

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_STATIC_EXCLUDED_PATHS` | `kernel.static.excluded_paths` | `""`    |

The value is a comma-separated list whose entries are trimmed of surrounding whitespace, so `/admin, /api/internal` names two prefixes while a blank or whitespace-only value names none. An entry must begin with `/` and may not be empty; validation rejects both at boot (`static excluded path must begin with a slash`, `static excluded path may not be empty`) rather than let a stray comma — which prefix-matches every path — silently take static serving out of service. An entry holding a percent escape other than `%2F`, `/a%20b`, is refused too, naming the key and the index, in every mode: the file server compares an exclusion with the request path decoded, so such an entry would exclude only a path whose name holds the percent sign; write it decoded. See [HTTP](HTTP.md) for what a declined request goes on to do, and for why this is the lever an application pulls to serve part of the url space behind middleware of its own.

### Cli boot warnings

What a command writes of the [boot warnings](SECURITY.md#boot-warnings) the boot collected. Read through [`CliConfiguration`](../../config/contract/cli.go), `BootWarningsIgnored()` and `BootWarningsDisabled()`.

| Environment key                     | Parameter name                      | Default        |
|-------------------------------------|-------------------------------------|----------------|
| `MELODY_CLI_BOOT_WARNINGS_IGNORED`  | `kernel.cli_boot_warnings_ignored`  | `""`           |
| `MELODY_CLI_BOOT_WARNINGS_DISABLED` | `kernel.cli_boot_warnings_disabled` | `""` (`false`) |

A command writes every boot warning it collected once, before its own output, into its configured journal, stderr when `MELODY_LOG_PATH` is empty; a process that dies before then writes them before its exit record, under the same two keys. `MELODY_CLI_BOOT_WARNINGS_IGNORED` is a comma-separated list of codes a command leaves out; `MELODY_CLI_BOOT_WARNINGS_DISABLED=true` writes none. The configuration refuses, naming the key and the index, an empty or repeated code and a list beside `DISABLED=true`; the boot refuses, after every module declared its codes, a code neither the framework nor a registered module writes and a code only an http process writes, since each would silence nothing. The codes a command can write are those the [Boot warnings](SECURITY.md#boot-warnings) table marks for cli, `application.environment.undeclaredKey` among them, plus those a registered [`BootWarningModule`](APPLICATION.md) declares for cli. Both keys belong to the command's environment: an http process that sets either is refused at boot. A module's service built outside the module, which names its hazard at its first use, honours both keys too.

### Typed accessors

[`Parameter`](../../config/parameter.go) reads its value through `MustString`, `Bool`, `Int`, `Float` and `Duration`, converting from the native type or from the string an environment value always arrives as. `Int`, `Float` and `Duration` read through the shared parser, so a whole number registered at runtime as an `int64` — or as an integral `float64` — converts through `Int` and `Float`; `Duration` **refuses the bare integer**, because it carries no unit, and wants a `time.Duration` or a string such as `"30s"`. `Float` refuses NaN and the infinities, which `strconv.ParseFloat` parses from `"NaN"`, `"Inf"` and `"Infinity"` without an error and which disarm every threshold written the normal way, each ordered comparison against NaN being false. The **string** grammar `Float` reads is plain decimal — an optional sign, digits and at most one decimal point — where `strconv.ParseFloat` alone also reads underscore spellings (`"1_000.5"`), hexadecimal floats (`"0x1p10"` is `1024`) and
exponents (`"1e3"`): a value the strict base-10 `Int` refuses is refused as a float too, instead of being silently accepted under a spelling nobody meant to support. The frozen majors keep the full `ParseFloat` grammar with this difference documented. Each fallible accessor reports an unset or non-convertible value as an error identified by the environment key where there is one and by the registration name otherwise, keeping an inline credential out of the exception context; a **secret** parameter additionally withholds the parse failure underneath, because strconv quotes the value it refused.

## Container integration

The package defines the service name:

- [`ServiceConfig`](../../config/service_resolver.go) (`"service.config"`)

If you want other services to resolve a configuration from the container, register `ServiceConfig` as a `configcontract.Configuration` provider and use `ConfigMustFromResolver`.

## Usage

The example below demonstrates loading an environment from `.env` files, creating a configuration, resolving it, and registering it into the container.

```go
package main

import (
    "context"
    "os"

    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func registerConfiguration(
    serviceContainer containercontract.Container,
    projectDirectory string,
) configcontract.Configuration {
    projectFileSystem := os.DirFS(
        projectDirectory,
    )

    environmentSource := config.NewEnvironmentSource(
        config.EnvironmentSourceConfig{
            FileSystem: projectFileSystem,
        },
    )

    environment, environmentErr := config.NewEnvironment(
        environmentSource,
    )
    if nil != environmentErr {
        exception.Panic(
            exception.NewError("failed to create environment", nil, environmentErr),
        )
    }

    configuration, configurationErr := config.NewConfiguration(
        environment,
        projectDirectory,
        config.ConfigurationConfig{},
    )
    if nil != configurationErr {
        exception.Panic(
            exception.NewError("failed to create configuration", nil, configurationErr),
        )
    }

    configuration.RegisterRuntime(
        "runtime.context",
        context.Background(),
    )

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        exception.Panic(
            exception.NewError("failed to resolve configuration", nil, resolveErr),
        )
    }

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
        container.RegisterOptions{},
    )

    return configuration
}

func example() configcontract.Configuration {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return registerConfiguration(
        serviceContainer,
        "/path/to/project",
    )
}
```

## Footguns & caveats

- `Application.Boot()` calls `Resolve()` after all runtime parameters are registered via `Application.RegisterParameter`.
- The boot `Resolve()` passes through any parameter whose value is not a string — which is what a runtime parameter registered with a native value is; a runtime parameter registered with a string template is resolved like any other, eagerly at registration once the boot resolution has run.
- A template reference (e.g., `%kernel.project_directory%`, `%env(MELODY_ENV)%`) resolves only against a parameter whose value is a string; a parameter registered after construction is deferred by the constructor's tolerant pass and settled by the boot `Resolve()`, so the composition root may reference what it registers before boot. The deferral is **not** transparent while it lasts: between construction and boot every value accessor on a deferred parameter panics naming it, because the alternative is handing out the raw `%app.user%` template as though it were the value; `Template` alone answers the raw template, as it does for every parameter. Read such a parameter after `Resolve()` has run. A runtime registration made before boot is deferred only when its value carries a template construct — a doubled percent, an `%env(...)%`, a `%name%` reference or a name-shaped run a percent opened; a literal such as `Coverage 95%` is its own resolved value and reads at once.

## Userland API

### Core types (`config`)

- [`type Configuration`](../../config/configuration.go)
    - [`NewConfiguration(*Environment, string, ConfigurationConfig) (*Configuration, error)`](../../config/configuration.go)
    - `Get(name string) configcontract.Parameter`
    - `MustGet(name string) configcontract.Parameter`
    - `RegisterRuntime(name string, value any)`
    - `RegisterRuntimeSecret(name string, value any)`
    - `MarkSecret(name string) bool`
    - `Resolve() error`
    - [`MarkServing(token)`](../../config/configuration.go) — called by `Application.Run` alone, the token one only the framework can build, any other refused: records that the wiring phase is over, after which `Resolve` is refused: services built during boot hold the values they read, so re-resolving reconfigures nothing and only rewrites the parameter store under readers that expect it settled. Registering a parameter still works, since a registration resolves itself
    - `Names() []string`
    - `Kernel() configcontract.KernelConfiguration`
    - `Http() configcontract.HttpConfiguration`
    - `Cli() configcontract.CliConfiguration`
    - [`DeclareEnvironmentKey(owner string, key configcontract.EnvironmentKey) error` / `CheckEnvironmentKeys() error` / `UndeclaredEnvironmentKeys() []string`](../../config/environment_key.go) — the keys a module declares, judged after the resolve, and the `MELODY_*` keys nobody declared (see [Module keys](#module-keys))
    - [`EnvironmentValue(key string) (string, bool)`](../../config/environment_key.go) — the text an artifact loaded for the key, a blank line counted as set, no declared default; raw text, no secret-safe door
- [`LimitKeyRule[T bound.Unit](bound.Rule[T])`, `SwitchKeyRule()`, `ChoiceKeyRule([]string)`](../../config/environment_key_rule.go) — the rules of a module's key
- [`ResolveLimitParameter[T bound.Unit](configcontract.Parameter, string, bound.Rule[T]) (bound.Resolved[T], error)`, `ParameterSwitch(configcontract.Parameter, string) (configcontract.Switch, error)`](../../config/environment_key_rule.go) — the module's read of a limit and of a boolean, the parsers the boot check runs
- [`type Environment`](../../config/environment.go)
    - [`NewEnvironment(configcontract.EnvironmentSource) (*Environment, error)`](../../config/environment.go)
    - [`All() map[string]string` / `Get(key string) (string, bool)`](../../config/environment.go) — the loaded `.env` keys, and one of them with the presence flag that tells an empty value apart from an absent one
- [`type EnvironmentSource`](../../config/environment_source.go)
    - [`NewEnvironmentSource(EnvironmentSourceConfig) *EnvironmentSource`](../../config/environment_source.go) — `FileSystem` and `BaseDirectory`, the directory inside it, an empty one reading as its root
- [`type Parameter`](../../config/parameter.go), [`type ParameterMap`](../../config/parameter.go) — built by the configuration alone; a parameter of the application's own is registered through `RegisterRuntime` and read back through `Get`
- [`IntWithDefault(configParameter configcontract.Parameter, defaultValue int) int`](../../config/utility.go) — answers the default only for a parameter that is **absent**: one that exists and does not parse panics, because a mistyped value that quietly becomes a number nobody wrote is a misconfiguration running in disguise

### Container helpers (`config`)

- [`const ServiceConfig`](../../config/service_resolver.go)
- [`ConfigFromRuntime(containercontract.Runtime) (configcontract.Configuration, error)`](../../config/service_resolver.go) — the error form of `ConfigMustFromRuntime`
- [`ConfigMustFromRuntime(containercontract.Runtime) configcontract.Configuration`](../../config/service_resolver.go) — the door of code that holds a runtime: a handler, a command, a listener, a boot hook under `kernelInstance.BootRuntime()`
- [`ConfigMustFromResolver(context.Context, containercontract.Resolver) configcontract.Configuration`](../../config/service_resolver.go) — the door a provider comes through, since a provider is handed a `Resolver`; it takes the container as it takes a scope

### Environment variable keys (`config`)

- [`EnvironmentNameKey`, `DefaultModeKey`, `ProcessRoleKey`, `HttpAddressKey`, `HttpMaxRequestBodyBytesKey`, `HttpSessionTtlKey`, `HttpSessionTombstoneRetentionKey`, `HttpSessionMaxSessionsKey`, `HttpShutdownTimeoutKey`, `HttpUnavailableRetryAfterKey`, `HttpSessionEmptyOnOutageKey`, `HttpSessionCookieSecureKey`, `TeardownTimeoutKey`, `ScopeTeardownTimeoutKey`, `ContainerCreationWaitersKey`, `ContainerTeardownSequentialKey`, `CliNameKey`, `CliDescriptionKey`, `CliBootWarningsIgnoredKey`, `CliBootWarningsDisabledKey`, `LogPathKey`, `LogLevelKey`, `DefaultLocaleKey`, `PublicDirectoryKey`, `StaticIndexFileKey`, `StaticCacheDisabledKey`, `StaticCacheMaxAgeKey`, `StaticExcludedPathsKey`](../../config/environment.go)

### Kernel parameter names (`config`)

- [`KernelDefaultMode`, `KernelProcessRole`, `KernelEnvironment`, `KernelHttpAddress`, `KernelHttpMaxRequestBodyBytes`, `KernelHttpSessionTtl`, `KernelHttpSessionTombstoneRetention`, `KernelHttpSessionMaxSessions`, `KernelHttpShutdownTimeout`, `KernelHttpUnavailableRetryAfter`, `KernelHttpSessionEmptyOnOutage`, `KernelHttpSessionCookieSecure`, `KernelTeardownTimeout`, `KernelScopeTeardownTimeout`, `KernelContainerCreationWaiters`, `KernelContainerTeardownSequential`, `KernelCliName`, `KernelCliDescription`, `KernelCliBootWarningsIgnored`, `KernelCliBootWarningsDisabled`, `KernelLogPath`, `KernelLogLevel`, `KernelDefaultLocale`, `KernelPublicDirectory`, `KernelStaticIndexFile`, `KernelStaticCacheDisabled`, `KernelStaticCacheMaxAge`, `KernelStaticExcludedPaths`, `KernelProjectDirectory`, `KernelLogsDirectory`, `KernelCacheDirectory`](../../config/environment.go)

### Environment / mode / role constants (`config`)

- [`EnvironmentDevelopment`, `EnvironmentProduction`](../../config/environment.go)
- [`ModeHttp`, `ModeCli`](../../config/environment.go)
- [`HttpSessionCookieSecureAlways`](../../config/environment.go) — the one value `MELODY_HTTP_SESSION_COOKIE_SECURE` admits
- [`RoleWeb`, `RoleWorker`, `RoleAll`](../../config/environment.go)
- [`RoleAllowsBackgroundWork(role string) bool`](../../config/process_role.go) — whether a process with this role should start outbox relays and message consumers. Melody itself gates nothing on the role: it is declared intent for application wiring to query
- [`RoleAllowsHttp(role string) bool`](../../config/process_role.go) — informational in the same way; the runtime **mode**, not the role, decides whether the http kernel starts

### Contracts (`config/contract`)

- [`type Configuration`](../../config/contract/configuration.go)
- [`type KernelConfiguration`](../../config/contract/kernel.go)
- [`type HttpConfiguration`](../../config/contract/http.go)
- [`type CliConfiguration`](../../config/contract/cli.go)
- [`type EnvironmentSource`](../../config/contract/environment_source.go)
- [`type Parameter`](../../config/contract/parameter.go)
- [`type EnvironmentKey`, `type EnvironmentKeyRule`, `type EnvironmentKeyDeclarer`, `type RetiredEnvironmentKey`, `type OverriddenEnvironmentKey`](../../config/contract/environment_key.go) — a module's key, its rule, the capability of a configuration that holds them, which `Configuration` does not demand, and a retired spelling the `.env` artifacts set
