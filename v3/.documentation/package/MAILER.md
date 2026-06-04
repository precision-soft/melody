# MAILER

The [`mailer`](../../mailer) package sends email through a pluggable transport. It ships a dependency-free SMTP transport built on the standard library and an in-memory transport for tests.

## Scope

Mailing is opt-in. Userland builds a `Mailer` over a `Transport` and registers it under [`ServiceMailer`](../../mailer/service_resolver.go). The package builds RFC 5322 / MIME messages and sends them; provider-specific transports (for example a hosted email API) can implement the same [`Transport`](../../mailer/contract/mailer.go) contract as integrations.

## Subpackages

- [`mailer/contract`](../../mailer/contract)  
  Public contracts for the mailer, transport, message, and address types.

## Responsibilities

- Define the abstraction:
    - [`Mailer`](../../mailer/contract/mailer.go), [`Transport`](../../mailer/contract/mailer.go)
    - [`Message`](../../mailer/contract/mailer.go), [`Address`](../../mailer/contract/mailer.go)
- Orchestrate sending with validation:
    - [`Manager`](../../mailer/manager.go), [`NewManager`](../../mailer/manager.go)
- Render and transport messages:
    - [`RenderMessage`](../../mailer/message.go) (RFC 5322 headers; `multipart/alternative` when both text and HTML bodies are set)
    - [`SmtpTransport`](../../mailer/smtp_transport.go), [`NewSmtpTransport`](../../mailer/smtp_transport.go)
    - [`InMemoryTransport`](../../mailer/in_memory_transport.go), [`NewInMemoryTransport`](../../mailer/in_memory_transport.go)
- Provide container resolver helpers:
    - [`ServiceMailer`](../../mailer/service_resolver.go)
    - [`MailerMustFromContainer`](../../mailer/service_resolver.go), [`MailerMustFromResolver`](../../mailer/service_resolver.go)

## Message rendering

[`RenderMessage`](../../mailer/message.go) writes standard headers (`From`, `To`, `Cc`, `Reply-To`, `Subject`, `Date`, `MIME-Version`) plus any custom `Headers`, then a body chosen from the populated fields:

- both `Text` and `Html` → `multipart/alternative` with a `text/plain` and a `text/html` part;
- only `Html` → `text/html`;
- otherwise → `text/plain`.

`Bcc` recipients are included in the SMTP envelope but never written to headers.

## Usage

```go
transport := mailer.NewSmtpTransport(mailer.SmtpConfig{
	Address:  "localhost:1025",
	Username: "",
	Password: "",
})

mailerInstance := mailer.NewManager(transport)

sendErr := mailerInstance.Send(runtimeInstance, mailercontract.Message{
	From:    mailercontract.Address{Name: "Shop", Email: "shop@example.com"},
	To:      []mailercontract.Address{{Email: "ada@example.com"}},
	Subject: "Welcome",
	Text:    "Welcome to the shop!",
	Html:    "<p>Welcome to the shop!</p>",
})
```

For tests, swap in [`InMemoryTransport`](../../mailer/in_memory_transport.go) and assert on `Sent()`. The example application registers an SMTP-backed mailer (`config/mailer.go`) and a `mailer:send` command (`cli/mail_send_command.go`).

## Footguns & caveats

- Mailing is opt-in and userland-wired; the framework registers no default mailer.
- [`NewSmtpTransport`](../../mailer/smtp_transport.go) uses `net/smtp.SendMail`, which issues `STARTTLS` when the server advertises it and authenticates only when a username is set. It does not support implicit TLS (SMTPS on port 465); use an integration transport for that.
- [`Manager.Send`](../../mailer/manager.go) requires a sender and at least one recipient; bodies and subjects are otherwise unvalidated.
- Header and address values are written verbatim — callers are responsible for not injecting CRLF into untrusted header content.

## Userland API

### Contracts (`mailer/contract`)

- [`Mailer`](../../mailer/contract/mailer.go)
- [`Transport`](../../mailer/contract/mailer.go)
- [`Message`](../../mailer/contract/mailer.go)
- [`Address`](../../mailer/contract/mailer.go)

### Types and constructors (`mailer`)

- [`Manager`](../../mailer/manager.go) — [`NewManager(transport mailercontract.Transport) *Manager`](../../mailer/manager.go)
- [`SmtpTransport`](../../mailer/smtp_transport.go) / [`SmtpConfig`](../../mailer/smtp_transport.go) — [`NewSmtpTransport(config SmtpConfig) *SmtpTransport`](../../mailer/smtp_transport.go)
- [`InMemoryTransport`](../../mailer/in_memory_transport.go) — [`NewInMemoryTransport() *InMemoryTransport`](../../mailer/in_memory_transport.go), [`(*InMemoryTransport).Sent() []mailercontract.Message`](../../mailer/in_memory_transport.go)
- [`RenderMessage(message mailercontract.Message) ([]byte, error)`](../../mailer/message.go)

### Container helpers (`mailer`)

- [`const ServiceMailer`](../../mailer/service_resolver.go)
- [`MailerMustFromContainer(containercontract.Container) mailercontract.Mailer`](../../mailer/service_resolver.go)
- [`MailerMustFromResolver(containercontract.Resolver) mailercontract.Mailer`](../../mailer/service_resolver.go)
