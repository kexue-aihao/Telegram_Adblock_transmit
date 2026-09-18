package domain

// BuiltinSettings overrides the startup defaults once saved by an administrator.
// An absent rule ID is enabled, including new detectors added by future releases.
type BuiltinSettings struct {
	Enabled       bool
	DisabledRules []string
}
