package mailer

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

const ServiceMailer = "service.mailer.mailer"

func MailerMustFromRuntime(runtimeInstance containercontract.Runtime) mailercontract.Mailer {
    return runtime.MustFromRuntime[mailercontract.Mailer](runtimeInstance, ServiceMailer)
}

func MailerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) mailercontract.Mailer {
    return container.MustFromResolver[mailercontract.Mailer](ctx, resolver, ServiceMailer)
}
