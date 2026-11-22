# TRANSLATION

The [`translation`](../../translation) package provides Melody's internationalization: locale-aware message catalogs with a fallback chain and an ICU-MessageFormat subset (named placeholders, `plural`, and `select`). It has no external dependencies.

## Scope

Translation is opt-in. Like the message bus, it is not wired by the application container; userland builds a `Translator` (in code or from files) and registers it under [`ServiceTranslator`](../../translation/service_resolver.go). The translator takes an explicit locale on every call, so it does not depend on kernel locale resolution.

## Subpackages

- [`translation/contract`](../../translation/contract)\
  Public contracts for the translator, catalogs, and loaders.

## Responsibilities

- Translate messages:
    - [`Translator`](../../translation/contract/translator.go)
    - [`Manager`](../../translation/manager.go)
    - [`NewManager`](../../translation/manager.go)
    - [`ManagerConfig`](../../translation/manager.go)
    - [`TranslateConfig`](../../translation/contract/translator.go)
    - [`ErrMessageNotFound`](../../translation/manager.go)
- Hold messages per locale and domain:
    - [`Catalog`](../../translation/contract/catalog.go)
    - [`MapCatalog`](../../translation/catalog.go)
    - [`NewMapCatalog`](../../translation/catalog.go)
    - [`DefaultDomain`](../../translation/catalog.go)
- Load catalogs from files:
    - [`Loader`](../../translation/contract/catalog.go)
    - [`JsonDirectoryLoader`](../../translation/json_loader.go)
    - [`NewJsonDirectoryLoader`](../../translation/json_loader.go)
- Provide container resolver helpers:
    - [`ServiceTranslator`](../../translation/service_resolver.go)
    - [`TranslatorMustFromResolver`](../../translation/service_resolver.go)

## Locale resolution

`Translate` and `HasMessage` resolve a message by walking a locale chain until a catalog contains the message id for the requested domain:

1. the requested locale (for example `ro-RO`);
2. its base locale (`ro`);
3. the configured fallback locales, in order;
4. the default locale.

Both take the message id and a [`TranslateConfig`](../../translation/contract/translator.go)`{Parameters, Domain, Locale}`; its zero value is no parameter, the default domain and the default locale chain, and `HasMessage` reads its domain and locale, never its parameters. An empty domain resolves to [`DefaultDomain`](../../translation/catalog.go) (`messages`).

`Translate` tells three outcomes apart: the rendered message and nil; for a message no catalog provides, the message id unchanged and an error matching [`ErrMessageNotFound`](../../translation/manager.go) through `errors.Is`, so a template can still print the id; for a pattern that cannot be rendered whole, the empty string and an error naming the message id, the domain, the resolved locale, the rune `offset` and the `reason`. `HasMessage` is the cheap probe that renders nothing.

## Message format

Patterns use an ICU-MessageFormat subset:

- Named placeholders: `Hello, {name}!`
- Plural: `{count, plural, =0 {No items} one {# item} other {# items}}` — `=N` selectors match exact numbers, `#` is replaced by the number, and the plural category is chosen per locale: the default `n == 1 → one` rule, plus CLDR-aligned categories for Romanian/Moldovan (`one`/`few`/`other`) and Russian (`one`/`few`/`many`/`other`).
- Select: `{gender, select, male {He} female {She} other {They}}`

Submessages are interpolated recursively, so placeholders and `#` work inside `plural`/`select` blocks. Inside a plural block `'#'` is a literal `#` and `''` a literal apostrophe.

## Container integration

The package defines the service name [`ServiceTranslator`](../../translation/service_resolver.go) (`"service.translation.translator"`). It is not registered by the framework; userland registers it in a `ServiceModule`.

## Usage

```go
package main

import (
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/translation"
    translationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

func buildTranslator(configuration configcontract.Configuration) *translation.Manager {
    english := translation.NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    english.Add("messages", "cart.items", "{count, plural, =0 {Your cart is empty} one {# item} other {# items}}")

    romanian := translation.NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    return translation.NewManager(translation.ManagerConfig{
        Catalogs:        []translationcontract.Catalog{english, romanian},
        FallbackLocales: []string{configuration.Http().DefaultLocale()},
    })
}
```

From a handler, resolve the service and translate with the request locale:

```go
translator := translation.TranslatorMustFromRuntime(runtimeInstance)

greeting, translateErr := translator.Translate(
    "greeting",
    translationcontract.TranslateConfig{Parameters: map[string]any{"name": "Ada"}, Locale: "ro"},
)
if nil != translateErr && false == errors.Is(translateErr, translation.ErrMessageNotFound) {
    return nil, translateErr
}
```

The example application wires a translator (`config/translation.go`) and exposes a public `/{locale}/i18n/greeting/` endpoint (`handler/i18n/greeting_handler.go`) whose locale is the path's first segment, validated against the route's locale list; it shows the id of a message no catalog carries and answers any other translation error as the request's failure.

## Footguns & caveats

- Translation is opt-in and userland-wired; the framework registers no default translator.
- Inside a `plural` branch, a `select` nested in it included, every bare `#` is the number; `'#'` writes a literal `#` and `''` a literal apostrophe, ICU's apostrophe quoting for that one character: `{n, plural, other {Order '#'{id}}}` renders `Order #A1` for `id` `A1`. Any other apostrophe is text, and outside a plural branch nothing is quoted. A `'#` no apostrophe closes is refused by `ValidatePattern`, so a pattern written for the third major that holds `'#'` in a branch renders `#` where it rendered `'5'`.
- A `plural` or `select` style must be keyword-and-block pairs and carry an `other` block: [`ValidatePattern`](../../translation/message_format.go) refuses a selector keyword with no block (`{n, plural, one other {x}}`), a block with no keyword (`{g, select, {x} other {y}}`), a style with no `other` and a selector nested deeper than the 32 levels `Translate` renders, whose blocks it would write as raw text, naming the rune offset and the reason, nested blocks included. [`MapCatalog.Add`](../../translation/catalog.go) refuses such a pattern with a panic naming the locale, the domain, the message id, the offset and the reason, and [`JsonDirectoryLoader.Load`](../../translation/json_loader.go) returns the same refusal naming the file and the directory too, so a malformed message fails the boot. A lone `{` or `}` in plain text, an argument left open and an unknown argument type are not refused: they render visibly, and the format has no quoting to write a literal brace otherwise.
- A [`Catalog`](../../translation/contract/catalog.go) of the application's own is judged by `Translate` at each call: a pattern `ValidatePattern` refuses answers the empty string and its error, every time it is asked. A catalog of its own runs each pattern through `ValidatePattern` when it is built, so the defect fails the boot instead.
- A locale is matched in BCP 47 case at every door, a catalog's, a fallback and the one `Translate` asks: the language lower, a four-letter script title, a two-letter region upper, every other subtag lower, so a catalog registered as `ro-RO` answers `ro-ro` and `RO-RO`, and `zh-Hant-TW` answers `ZH-HANT-tw`. The separators are kept as written: `ro_RO` and `ro-RO` are two locales. Two catalogs spelling one locale differently (`ro-RO` beside `ro-ro`) are refused by `NewManager`, naming both spellings; several catalogs spelling it alike assemble the locale, as before.
- Plural categories follow CLDR-aligned rules for the locales with dedicated support — Romanian/Moldovan (`one`/`few`/`other`) and Russian (`one`/`few`/`many`/`other`) — and fall back to the `n == 1 → one` rule for every other locale. Use `=N` selectors for exact cases that matter.
- A `plural` argument that is present but non-numeric — a `nil`, or a string that does not parse as a number (`"5"` takes its numeric category) — resolves the `other` branch and substitutes `#` with an empty string, rather than being treated as the number zero; an ABSENT argument renders the visible placeholder, as the next bullet says. This differs from ICU/MessageFormat implementations that default a missing argument to `0` — supply the argument explicitly if you depend on a numeric category.
- A placeholder whose parameter is absent renders as the visible placeholder itself (`Hello, {name}!`) — the plain placeholder, and a `plural` or `select` argument alike (`{count}`, `{gender}`) — so a renamed parameter key is observable in the output instead of silently deleting the value or the count; a parameter present with an explicit `nil` renders as the empty string, or resolves the `other` branch of a `plural`, because the caller said so. A `select` argument whose keyword matches no branch resolves the `other` branch, which every pattern a catalog of the framework holds carries.
- `MapCatalog.Add` is a boot-time builder with no synchronization: build catalogs, hand them to `NewManager`, and stop writing — an `Add` concurrent with any `Translate` is a data race. The `Manager` itself is immutable after construction and safe for concurrent use.
- Several catalogs per locale: [`NewManager`](../../translation/manager.go) keeps every catalog of a locale, in the order given, and a lookup asks them in that order until one answers, so a locale assembled from several sources — catalogs wired by hand, or loaded from two directories; the json loader itself merges the files of one directory into one catalog per locale, refusing two files that spell one locale differently and naming both — loses nothing, and a message two catalogs hold is answered by the earlier one. A catalog whose `Locale()` is empty is refused at construction, as a nil catalog is: the locale chain never asks for the empty locale.
- `JsonDirectoryLoader` expects files named `<domain>.<locale>.json` containing a flat `{messageId: message}` object; a `.json` file whose name does not parse that way fails the load naming the file, so a naming typo cannot ship a translator answering raw message ids (directories and non-`.json` files are simply ignored). There is no Manager bridge helper; load file-based catalogs explicitly and pass them into the constructor: `catalogs, err := translation.NewJsonDirectoryLoader(dir).Load(ctx)` then `translation.NewManager(translation.ManagerConfig{Catalogs: catalogs, FallbackLocales: []string{"en"}})`. `Load` judges its context before it reads the directory and before each file, so a done context answers its error, wrapped with the directory, and no catalog. A loaded set that lacks a fallback locale is refused at construction.

## Userland API

### Contracts (`translation/contract`)

- [`Translator`](../../translation/contract/translator.go) — `Translate(messageId string, config TranslateConfig) (string, error)`, `HasMessage(messageId string, config TranslateConfig) bool`
- [`TranslateConfig`](../../translation/contract/translator.go)
- [`Catalog`](../../translation/contract/catalog.go)
- [`Loader`](../../translation/contract/catalog.go) — `Load(ctx context.Context) ([]Catalog, error)`

### Variables (`translation`)

- [`ErrMessageNotFound`](../../translation/manager.go)

### Types (`translation`)

- [`Manager`](../../translation/manager.go)
- [`ManagerConfig`](../../translation/manager.go)
- [`MapCatalog`](../../translation/catalog.go)
- [`JsonDirectoryLoader`](../../translation/json_loader.go)

### Constructors (`translation`)

- [`NewManager(config ManagerConfig) *Manager`](../../translation/manager.go) — `ManagerConfig{Catalogs, FallbackLocales}`, a refusal naming its `field`; nil catalogs is a manager with no catalog, which answers raw ids; a lookup asks the locale, its base, then the fallbacks in order and nothing after the last, so an application ends the list on its default locale, `kernel.default_locale`; once a catalog is handed, an empty `FallbackLocales` is refused, as a fallback locale that is empty or that no catalog carries is, since the lookup asks them by that name in BCP 47 case and a region form like `en-US` does not reach an `en` catalog
- [`NewMapCatalog(locale string) *MapCatalog`](../../translation/catalog.go)
- [`NewJsonDirectoryLoader(directory string) *JsonDirectoryLoader`](../../translation/json_loader.go)

### Functions (`translation`)

- [`ValidatePattern(pattern string) error`](../../translation/message_format.go) — the first `plural` or `select` whose style is not keyword-and-block pairs ending with an `other` block, its context naming `offset` (in runes) and `reason`; or the first nested deeper than the 32 levels `Translate` renders; or the first apostrophe before `#` in a plural block that is not `'#'` or `''#`; nil when every `select` and `plural` is keyword-and-block pairs with an `other` block, at most 32 deep, and every such apostrophe is quoted

### Container helpers (`translation`)

- [`const ServiceTranslator`](../../translation/service_resolver.go)
- [`TranslatorMustFromRuntime(containercontract.Runtime) translationcontract.Translator`](../../translation/service_resolver.go)
- [`TranslatorMustFromResolver(context.Context, containercontract.Resolver) translationcontract.Translator`](../../translation/service_resolver.go)
