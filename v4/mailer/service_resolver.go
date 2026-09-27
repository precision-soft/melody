package mailer

import (
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

const ServiceMailer = "service.mailer.mailer"

func MailerMustFromContainer(serviceContainer containercontract.Container) mailercontract.Mailer {
    return container.MustFromResolver[mailercontract.Mailer](serviceContainer, ServiceMailer)
}

func MailerMustFromResolver(resolver containercontract.Resolver) mailercontract.Mailer {
    return container.MustFromResolver[mailercontract.Mailer](resolver, ServiceMailer)
}
