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
    - [`RenderMessage`](../../mailer/message.go) (RFC 5322 headers; quoted-printable bodies; `multipart/alternative` for text+HTML, `multipart/related` for inline images, `multipart/mixed` when regular attachments are present)
    - [`SmtpTransport`](../../mailer/smtp_transport.go), [`NewSmtpTransport`](../../mailer/smtp_transport.go)
    - [`InMemoryTransport`](../../mailer/in_memory_transport.go), [`NewInMemoryTransport`](../../mailer/in_memory_transport.go)
    - [`LogTransport`](../../mailer/log_transport.go), [`NewLogTransport`](../../mailer/log_transport.go)
- Provide container resolver helpers:
    - [`ServiceMailer`](../../mailer/service_resolver.go)
    - [`MailerMustFromContainer`](../../mailer/service_resolver.go), [`MailerMustFromResolver`](../../mailer/service_resolver.go)

## Message rendering

[`RenderMessage`](../../mailer/message.go) writes standard headers (`From`, `To`, `Cc`, `Reply-To`, `Subject`, `Date`, `MIME-Version`) plus any custom `Headers`, then a body chosen from the populated fields:

- both `Text` and `Html` → `multipart/alternative` with a `text/plain` and a `text/html` part;
- only `Html` → `text/html`;
- otherwise → `text/plain`.

Text bodies are `quoted-printable` encoded so every output line stays within the SMTP 998-character limit and 8-bit UTF-8 is transported safely. Attachments are partitioned by their `ContentId`: an attachment with a non-empty `ContentId` is **inline** and an attachment without one is a **regular** file. When regular attachments are present the whole message is wrapped in `multipart/mixed` — the body entity above becomes the first part, and each regular attachment follows as a `base64`-encoded part with a `Content-Disposition: attachment` header. When any inline attachment is present, the body entity and the inline parts are grouped into a `multipart/related` entity (each inline part carries `Content-ID: <id>` and `Content-Disposition: inline`, so an HTML body can reference it as `<img src="cid:id">`); that related entity is the whole message when there are no regular attachments, or the first part of the `multipart/mixed` wrapper when regular attachments coexist. A `ContentId` supplied
without angle brackets is wrapped automatically; a `ContentId` containing whitespace or a control character, or one too long to fit on a single 998-octet header line, is rejected (a Content-ID is a single msg-id token that those would corrupt).

Custom `Headers` whose name collides with a header the renderer emits itself (`Content-Type`, `MIME-Version`, `Content-Transfer-Encoding`, `Content-Disposition`, `From`, `To`, `Cc`, `Bcc`, `Reply-To`, `Subject`, `Date`) are dropped so a caller cannot duplicate or override the structural headers.

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

For tests, swap in [`InMemoryTransport`](../../mailer/in_memory_transport.go) and assert on `Sent()`. The example application wires its mailer in [`config/mailer.go`](../../.example/config/mailer.go) and a `mailer:send` command in [`cli/mail_send_command.go`](../../.example/cli/mail_send_command.go); it defaults to [`LogTransport`](../../mailer/log_transport.go) — the dev environment ships no SMTP server — and switches to an SMTP-backed mailer only when `SMTP_ADDRESS` is set.

## Configuration

[`SmtpConfig`](../../mailer/smtp_transport.go) exposes three timeout tunables. `Timeout` and `DataTerminationTimeout` are resolved once in [`NewSmtpTransport`](../../mailer/smtp_transport.go); `DialTimeout` is stored as given and defaulted lazily by [`resolveDialTimeout`](../../mailer/smtp_transport.go) at dial time. The resolved values are the same either way — the difference only matters if you read the transport's fields directly, where an unset `DialTimeout` is still zero.

| Field                    | Bounds                                                                                                                                                                                                             | Default when zero                                       |
|--------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------|
| `DialTimeout`            | the TCP connect, the TLS handshake and the server's opening greeting                                                                                                                                               | `30s`                                                   |
| `Timeout`                | every step of the session after the greeting — hello, auth, `MAIL`/`RCPT`/`DATA`, each chunk of the payload write, `QUIT` — as a per-step deadline re-armed before each step (so a large message is not penalised) | `DialTimeout`, then `30s`                               |
| `DataTerminationTimeout` | the server's acknowledgment of the message-ending dot, which a scanning relay routinely delays far beyond any other reply (RFC 5321 allows ten minutes)                                                            | `4 ×` the resolved `Timeout`, raised to a floor of `2m` |

Because the deadline is re-armed per step and per payload chunk, `Timeout` measures *progress*, not total transfer time: a slow-but-alive link completes regardless of message size, while a stalled peer still fails within one `Timeout`.

## Footguns & caveats

- Mailing is opt-in and userland-wired; the framework registers no default mailer.
- [`NewSmtpTransport`](../../mailer/smtp_transport.go) issues `STARTTLS` when the server advertises it and authenticates when a username is set. `SmtpConfig` exposes fail-closed controls: `RequireTls` aborts the send if TLS cannot be negotiated, `RequireAuth` requires authentication and refuses to send when no username is configured, `ImplicitTls` dials SMTPS directly (port 465) instead of upgrading via `STARTTLS`, and `TlsConfig` overrides the TLS settings (otherwise the server name is taken from `Host`, falling back to the host in `Address`).
- [`Manager.Send`](../../mailer/manager.go) requires a sender and at least one recipient; bodies and subjects are otherwise unvalidated.
- Header names/values and address fields have `CR`/`LF` stripped before they are written, and attachment filenames additionally have `"` stripped, so untrusted values cannot inject extra header lines or break out of the quoted `filename` parameter.
- A custom header whose value is a single no-space token longer than the encoded-word payload limit of 60 octets (a tracking id, a signed `List-Unsubscribe` URL, a JWT) is chunked into RFC 2047 encoded-words — the same protection `Subject` receives — so it stays under the 998-octet hard limit instead of emitting one over-long line strict MTAs reject. Short or whitespace-delimited values pass through byte-for-byte; only the pathological indivisible-token case is encoded, and it round-trips for RFC 2047-aware readers.
- **`Message-ID`, `In-Reply-To`, `References` and `Content-ID` are exempt from that chunking and are validated instead.** RFC 2047 §5 forbids encoded-words inside msg-id tokens, so these four headers are emitted intact ([`structuredIdentifierHeaders`](../../mailer/message.go)) — and because folding a single over-long token would hard-split it mid-token and corrupt the identifier, [`validateStructuredIdentifierHeader`](../../mailer/message.go) **returns an error** instead: `<name> header has an identifier token too long to encode on a single header line without corrupting it`. Any control character (including TAB and DEL — only a single space may separate tokens) is likewise an error. A long `References` chain therefore fails the send rather than being silently mangled; keep each individual msg-id token at 997 octets or fewer (the 998-octet hard line limit less the one leading space a continuation line carries), and note that a very long *chain* is fine because it folds at the spaces
  between its tokens.

## Userland API

### Contracts (`mailer/contract`)

- [`Mailer`](../../mailer/contract/mailer.go)
- [`Transport`](../../mailer/contract/mailer.go)
- [`Message`](../../mailer/contract/mailer.go)
- [`Address`](../../mailer/contract/mailer.go)
- [`Attachment`](../../mailer/contract/mailer.go) — `Filename`, `ContentType`, `Content`, and `ContentId` (a non-empty `ContentId` embeds the attachment inline for `<img src="cid:...">`)

### Types and constructors (`mailer`)

- [`Manager`](../../mailer/manager.go) — [`NewManager(transport mailercontract.Transport) *Manager`](../../mailer/manager.go)
- [`SmtpTransport`](../../mailer/smtp_transport.go) / [`SmtpConfig`](../../mailer/smtp_transport.go) — [`NewSmtpTransport(config SmtpConfig) *SmtpTransport`](../../mailer/smtp_transport.go)
- [`InMemoryTransport`](../../mailer/in_memory_transport.go) — [`NewInMemoryTransport() *InMemoryTransport`](../../mailer/in_memory_transport.go), [`(*InMemoryTransport).Sent() []mailercontract.Message`](../../mailer/in_memory_transport.go)
- [`LogTransport`](../../mailer/log_transport.go) — [`NewLogTransport(logger loggingcontract.Logger) *LogTransport`](../../mailer/log_transport.go); logs each message's recipients (To, Cc, Bcc), subject, both the text and HTML bodies, and per-attachment metadata (filename, content type, Content-ID, inline flag, byte size — never the raw content) at info level instead of delivering it (development aid). With a `nil` logger it resolves the request-scoped logger from the runtime, and is a safe no-op when neither is available.
- [`RenderMessage(message mailercontract.Message) ([]byte, error)`](../../mailer/message.go)

### Container helpers (`mailer`)

- [`const ServiceMailer`](../../mailer/service_resolver.go)
- [`MailerMustFromContainer(containercontract.Container) mailercontract.Mailer`](../../mailer/service_resolver.go)
- [`MailerMustFromResolver(containercontract.Resolver) mailercontract.Mailer`](../../mailer/service_resolver.go)
