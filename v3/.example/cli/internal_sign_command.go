package cli

import (
    "fmt"
    "time"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewInternalSignCommand mints the internal-auth (HMAC) header a caller service would send to reach the /internal firewall. It signs with the same shared secret the firewall verifies against, so the printed header authenticates. The signed method/path/body must match the request exactly, so pass the same values to curl. newSigner builds the signer for the envelope lifetime asked with --ttl, zero taking the signer's default. */
func NewInternalSignCommand(newSigner func(ttl time.Duration) *melodysecurity.HmacEnvelopeSigner) *InternalSignCommand {
    return &InternalSignCommand{newSigner: newSigner}
}

type InternalSignCommand struct {
    newSigner func(ttl time.Duration) *melodysecurity.HmacEnvelopeSigner
}

func (instance *InternalSignCommand) Name() string {
    return "internal:sign"
}

func (instance *InternalSignCommand) Description() string {
    return "mints an internal-auth (HMAC) header for the /internal firewall"
}

func (instance *InternalSignCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:  "method",
            Usage: "http method the envelope is bound to; defaults to POST",
        },
        &melodyclicontract.StringFlag{
            Name:  "path",
            Usage: "request path (with any query string) the envelope is bound to; defaults to /internal/whoami/",
        },
        &melodyclicontract.StringFlag{
            Name:  "body",
            Usage: "request body the envelope's hash is bound to; defaults to empty",
        },
        &melodyclicontract.StringFlag{
            Name:  "actor",
            Usage: "optional originating actor (user id) to propagate on behalf of",
        },
        &melodyclicontract.StringFlag{
            Name:  "ttl",
            Usage: "how long the envelope stays valid, as a go duration; defaults to the signer's 30s. It is not capped here: the firewall refuses an envelope whose expiry sits past its horizon, and an operator may mint one to see that refusal",
        },
    }
}

func (instance *InternalSignCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    method := commandContext.String("method")
    if "" == method {
        method = "POST"
    }

    path := commandContext.String("path")
    if "" == path {
        path = "/internal/whoami/"
    }

    body := []byte(commandContext.String("body"))

    var actor melodysecuritycontract.Actor
    if actorId := commandContext.String("actor"); "" != actorId {
        actor = melodysecurity.NewActor(
            actorId,
            melodysecuritycontract.ActorTypeUser,
            []string{"ROLE_BUYER"},
            map[string]string{"tenant": "acme"},
        )
    }

    ttl := time.Duration(0)
    if rawTtl := commandContext.String("ttl"); "" != rawTtl {
        parsedTtl, parseErr := time.ParseDuration(rawTtl)
        if nil != parseErr || 0 >= parsedTtl {
            return fmt.Errorf("internal:sign --ttl %q is not a positive go duration", rawTtl)
        }

        ttl = parsedTtl
    }

    signer := instance.newSigner(ttl)

    header, signErr := signer.Sign(method, path, body, actor)
    if nil != signErr {
        return signErr
    }

    writer := commandContext.Writer

    if _, writeErr := fmt.Fprintf(writer, "%s\n", signer.HeaderName()); nil != writeErr {
        return writeErr
    }

    _, writeErr := fmt.Fprintf(writer, "%s\n", header)

    return writeErr
}
