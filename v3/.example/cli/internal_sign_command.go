package cli

import (
    "fmt"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewInternalSignCommand mints the internal-auth (HMAC) header a caller service would send to reach the
/internal firewall. It signs with the same shared secret the firewall verifies against, so the printed
header authenticates. The signed method/path/body must match the request exactly, so pass the same values
to curl. */
func NewInternalSignCommand(signer *melodysecurity.HmacEnvelopeSigner) *InternalSignCommand {
    return &InternalSignCommand{signer: signer}
}

type InternalSignCommand struct {
    signer *melodysecurity.HmacEnvelopeSigner
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
    }
}

func (instance *InternalSignCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext melodyclicontract.Context,
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

    header, signErr := instance.signer.Sign(method, path, body, actor)
    if nil != signErr {
        return signErr
    }

    fmt.Printf("%s\n", instance.signer.HeaderName())
    fmt.Printf("%s\n", header)

    return nil
}
