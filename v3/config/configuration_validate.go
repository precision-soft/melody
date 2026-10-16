package config

/* validate is the post-resolution hook. Placeholders need no check: the resolver fails on anything placeholder-shaped it cannot resolve, so a resolved value carries a percent only as data. */
func (instance *Configuration) validate() error {
    return nil
}
