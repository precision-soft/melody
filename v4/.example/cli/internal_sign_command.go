package cli

import (
    "fmt"
    "strings"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v4/bound"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewInternalSignCommand mints the internal-auth (HMAC) header a caller service would send to reach the /internal firewall. It signs with the same shared secret the firewall verifies against, so the printed header authenticates. The signed method/path/body must match the request exactly, so pass the same values to curl. signer is the one built at boot, with the signer's default lifetime; newSigner builds one under the run's runtime only for the lifetime asked with --ttl. */
func NewInternalSignCommand(
    signer *melodycontainer.LazyService[*melodysecurity.HmacEnvelopeSigner],
    newSigner func(runtimeInstance melodycontainercontract.Runtime, ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner,
) *InternalSignCommand {
    return &InternalSignCommand{signer: signer, newSigner: newSigner}
}

type InternalSignCommand struct {
    signer    *melodycontainer.LazyService[*melodysecurity.HmacEnvelopeSigner]
    newSigner func(runtimeInstance melodycontainercontract.Runtime, ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner
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
            Name:  "actor-roles",
            Value: defaultInternalSignActorRoles,
            Usage: "the propagated actor's roles, comma-separated, each named once; empty for none; refused without --actor",
        },
        &melodyclicontract.StringFlag{
            Name:  "actor-attributes",
            Value: defaultInternalSignActorAttributes,
            Usage: "the propagated actor's attributes, comma-separated key=value pairs, each key named once; empty for none; refused without --actor",
        },
        &melodyclicontract.StringFlag{
            Name:  "ttl",
            Usage: "how long the envelope stays valid, as a go duration; defaults to the signer's 30s. It is not capped here: the firewall refuses an envelope whose expiry sits past its horizon, and an operator may mint one to see that refusal",
        },
    }
}

func (instance *InternalSignCommand) Run(
    runtimeInstance melodycontainercontract.Runtime,
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
    actorId := commandContext.String("actor")
    if "" == actorId {
        for _, actorFlag := range []string{"actor-roles", "actor-attributes"} {
            if true == commandContext.IsSet(actorFlag) {
                return fmt.Errorf("internal:sign --%s is read only with --actor, which is not set", actorFlag)
            }
        }
    }

    if "" != actorId {
        roleList, rolesErr := internalSignActorRoles(flagOrDefault(commandContext, "actor-roles", defaultInternalSignActorRoles))
        if nil != rolesErr {
            return rolesErr
        }

        attributes, attributesErr := internalSignActorAttributes(flagOrDefault(commandContext, "actor-attributes", defaultInternalSignActorAttributes))
        if nil != attributesErr {
            return attributesErr
        }

        actor = melodysecurity.NewActor(
            actorId,
            melodysecuritycontract.ActorTypeUser,
            roleList,
            attributes,
            nil,
        )
    }

    signer, signerErr := instance.signerFor(runtimeInstance, commandContext.String("ttl"))
    if nil != signerErr {
        return signerErr
    }

    header, signErr := signer.Sign(runtimeInstance, method, path, body, actor)
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

func (instance *InternalSignCommand) signerFor(runtimeInstance melodycontainercontract.Runtime, rawTtl string) (*melodysecurity.HmacEnvelopeSigner, error) {
    if "" == rawTtl {
        return instance.signer.Resolve(runtimeInstance)
    }

    parsedTtl, parseErr := time.ParseDuration(rawTtl)
    if nil != parseErr || bound.TtlMinimum > parsedTtl {
        return nil, fmt.Errorf("internal:sign --ttl %q is not a go duration of at least 1s", rawTtl)
    }

    return instance.newSigner(runtimeInstance, bound.Of(parsedTtl)), nil
}

const (
    defaultInternalSignActorRoles      = "ROLE_BUYER"
    defaultInternalSignActorAttributes = "tenant=acme"
)

func flagOrDefault(commandContext *melodyclicontract.CommandContext, name string, defaultValue string) string {
    if false == commandContext.IsSet(name) {
        return defaultValue
    }

    return commandContext.String(name)
}

/* an empty value is no role, distinct from the flag left out */
func internalSignActorRoles(value string) ([]string, error) {
    roleList := make([]string, 0)
    if "" == value {
        return roleList, nil
    }

    seen := map[string]bool{}
    for _, role := range strings.Split(value, ",") {
        if "" == role || -1 != strings.IndexFunc(role, unicode.IsSpace) || -1 != strings.IndexFunc(role, unicode.IsControl) {
            return nil, fmt.Errorf("internal:sign --actor-roles %q holds an empty role or a role with whitespace or a control character", value)
        }

        if true == seen[role] {
            return nil, fmt.Errorf("internal:sign --actor-roles %q names the role %q twice", value, role)
        }

        seen[role] = true
        roleList = append(roleList, role)
    }

    return roleList, nil
}

/* an empty value is no attribute, distinct from the flag left out */
func internalSignActorAttributes(value string) (map[string]string, error) {
    attributes := map[string]string{}
    if "" == value {
        return attributes, nil
    }

    for _, pair := range strings.Split(value, ",") {
        key, attributeValue, hasSeparator := strings.Cut(pair, "=")
        key = strings.TrimSpace(key)
        attributeValue = strings.TrimSpace(attributeValue)

        if false == hasSeparator || "" == key || "" == attributeValue {
            return nil, fmt.Errorf("internal:sign --actor-attributes %q holds an element that is not key=value", value)
        }

        if -1 != strings.IndexFunc(key, unicode.IsControl) || -1 != strings.IndexFunc(attributeValue, unicode.IsControl) {
            return nil, fmt.Errorf("internal:sign --actor-attributes %q holds a control character", value)
        }

        if _, exists := attributes[key]; true == exists {
            return nil, fmt.Errorf("internal:sign --actor-attributes %q names the key %q twice", value, key)
        }

        attributes[key] = attributeValue
    }

    return attributes, nil
}
