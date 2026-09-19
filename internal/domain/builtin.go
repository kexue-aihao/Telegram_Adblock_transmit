package domain

// BuiltinSettings overrides the startup defaults once saved by an administrator.
// An absent rule ID is enabled, including new detectors added by future releases.
type BuiltinSettings struct {
	Enabled       bool
	DisabledRules []string
}

// BuiltinHit contains an explanation made only of catalog metadata and short
// evidence labels. It must not contain message excerpts, contacts or URLs.
type BuiltinHit struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Evidence []string `json:"evidence"`
}

// BuiltinDetails preserves the rule library version used for an audit event.
// A nil details pointer represents a legacy event with hit IDs only.
type BuiltinDetails struct {
	LibraryVersion string       `json:"library_version"`
	Hits           []BuiltinHit `json:"hits"`
}
