package output

import (
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
)

/* MetadataApplicationVersion keys the declared version in the root command's metadata. */
const MetadataApplicationVersion = "melody.applicationVersion"

/* UnknownApplicationVersion is how a table and --version spell a version the application never declared. */
const UnknownApplicationVersion = "<unknown>"

/* ApplicationVersionOf answers the version declared on the root of the command's tree, "" when none. */
func ApplicationVersionOf(commandContext *clicontract.CommandContext) string {
    if nil == commandContext {
        return ""
    }

    declared, isText := commandContext.Root().Metadata[MetadataApplicationVersion].(string)
    if false == isText {
        return ""
    }

    return declared
}
