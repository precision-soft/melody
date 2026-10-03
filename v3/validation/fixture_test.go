package validation

/* pointerOf builds the optional value every constraint has to skip rather than refuse; every constraint suite in the package reaches it from here. */
func pointerOf(value string) *string {
    return &value
}
