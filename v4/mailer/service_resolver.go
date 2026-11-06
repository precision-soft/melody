package mailer

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

const ServiceMailer = "service.mailer.mailer"

func MailerMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) mailercontract.Mailer {
    return container.MustFromResolver[mailercontract.Mailer](ctx, serviceContainer, ServiceMailer)
}

func MailerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) mailercontract.Mailer {
    return container.MustFromResolver[mailercontract.Mailer](ctx, resolver, ServiceMailer)
}
