package output

var applicationVersion = ""

func SetApplicationVersion(versionString string) {
    applicationVersion = versionString
}

func getApplicationVersion() string {
    return applicationVersion
}
