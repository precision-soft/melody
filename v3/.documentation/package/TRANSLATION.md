# TRANSLATION

The [`translation`](../../translation) package provides Melody's internationalization: locale-aware message catalogs with a fallback chain and an ICU-MessageFormat subset (named placeholders, `plural`, and `select`). It has no external dependencies.

## Scope

Translation is opt-in. Like the message bus, it is not wired by the application container; userland builds a `Translator` (in code or from files) and registers it under [`ServiceTranslator`](../../translation/service_resolver.go). The translator takes an explicit locale on every call, so it does not depend on kernel locale resolution.

## Subpackages

- [`translation/contract`](../../translation/contract)  
  Public contracts for the translator, catalogs, and loaders.

## Responsibilities

- Translate messages:
    - [`Translator`](../../translation/contract/translator.go)
    - [`Manager`](../../translation/manager.go)
    - [`NewManager`](../../translation/manager.go)
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
    - [`TranslatorMustFromContainer`](../../translation/service_resolver.go)
    - [`TranslatorMustFromResolver`](../../translation/service_resolver.go)

## Locale resolution

`Trans` and `HasMessage` resolve a message by walking a locale chain until a catalog contains the message id for the requested domain:

1. the requested locale (for example `ro-RO`);
2. its base locale (`ro`);
3. the configured fallback locales, in order;
4. the default locale.

An empty domain resolves to [`DefaultDomain`](../../translation/catalog.go) (`messages`). When no catalog provides the message, `Trans` returns the message id unchanged.

## Message format

Patterns use an ICU-MessageFormat subset:

- Named placeholders: `Hello, {name}!`
- Plural: `{count, plural, =0 {No items} one {# item} other {# items}}` — `=N` selectors match exact numbers, `#` is replaced by the number, and the plural category is chosen per locale: the default `n == 1 → one` rule, plus CLDR-aligned categories for Romanian/Moldovan (`one`/`few`/`other`) and Russian (`one`/`few`/`many`/`other`).
- Select: `{gender, select, male {He} female {She} other {They}}`

Submessages are interpolated recursively, so placeholders and `#` work inside `plural`/`select` blocks.

## Container integration

The package defines the service name [`ServiceTranslator`](../../translation/service_resolver.go) (`"service.translation.translator"`). It is not registered by the framework; userland registers it in a `ServiceModule`.

## Usage

```go
package main

import (
	"github.com/precision-soft/melody/v3/translation"
)

func buildTranslator() *translation.Manager {
	english := translation.NewMapCatalog("en")
	english.Add("messages", "greeting", "Hello, {name}!")
	english.Add("messages", "cart.items", "{count, plural, =0 {Your cart is empty} one {# item} other {# items}}")

	romanian := translation.NewMapCatalog("ro")
	romanian.Add("messages", "greeting", "Salut, {name}!")

	return translation.NewManager("en", []string{"en"}, english, romanian)
}
```

From a handler, resolve the service and translate with the request locale:

```go
translator := translation.TranslatorMustFromContainer(runtimeInstance.Container())

greeting := translator.Trans("greeting", map[string]any{"name": "Ada"}, "messages", "ro")
```

The example application wires a translator (`config/translation.go`) and exposes a public `/i18n/greeting` endpoint (`handler/i18n/greeting_handler.go`) that translates by `?locale=` query.

## Footguns & caveats

- Translation is opt-in and userland-wired; the framework registers no default translator.
- Plural categories follow CLDR-aligned rules for the locales with dedicated support — Romanian/Moldovan (`one`/`few`/`other`) and Russian (`one`/`few`/`many`/`other`) — and fall back to the `n == 1 → one` rule for every other locale. Use `=N` selectors for exact cases that matter.
- One catalog per locale: [`NewManager`](../../translation/manager.go) keys catalogs by `Catalog.Locale()`, so passing two catalogs with the same locale keeps the last one.
- `JsonDirectoryLoader` expects files named `<domain>.<locale>.json` containing a flat `{messageId: message}` object. There is no Manager bridge helper; load file-based catalogs explicitly and pass them into the constructor: `catalogs, err := translation.NewJsonDirectoryLoader(dir).Load()` then `translation.NewManager("en", []string{"en"}, catalogs...)`.

## Userland API

### Contracts (`translation/contract`)

- [`Translator`](../../translation/contract/translator.go)
- [`Catalog`](../../translation/contract/catalog.go)
- [`Loader`](../../translation/contract/catalog.go)

### Types (`translation`)

- [`Manager`](../../translation/manager.go)
- [`MapCatalog`](../../translation/catalog.go)
- [`JsonDirectoryLoader`](../../translation/json_loader.go)

### Constructors (`translation`)

- [`NewManager(defaultLocale string, fallbackLocales []string, catalogs ...translationcontract.Catalog) *Manager`](../../translation/manager.go)
- [`NewMapCatalog(locale string) *MapCatalog`](../../translation/catalog.go)
- [`NewJsonDirectoryLoader(directory string) *JsonDirectoryLoader`](../../translation/json_loader.go)

### Container helpers (`translation`)

- [`const ServiceTranslator`](../../translation/service_resolver.go)
- [`TranslatorMustFromContainer(containercontract.Container) translationcontract.Translator`](../../translation/service_resolver.go)
- [`TranslatorMustFromResolver(containercontract.Resolver) translationcontract.Translator`](../../translation/service_resolver.go)
