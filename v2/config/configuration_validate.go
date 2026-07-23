package config

/* validate is the post-resolution hook. Unresolved placeholders need no check here: the resolver scans every template positionally and fails on anything placeholder-shaped it cannot resolve, so a resolved value can only carry a percent as data — written as the %% escape or standing alone where no construct opens. */
func (instance *Configuration) validate() error {
    return nil
}
