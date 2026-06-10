package presence

// The three presence/nullability combinations expressible across all
// extractor languages (SPEC §7).
type Presence struct {
	RequiredNonNull  string  `json:"requiredNonNull"`
	RequiredNullable *string `json:"requiredNullable"`          // pointer: present, may be null
	OptionalNonNull  string  `json:"optionalNonNull,omitempty"` // may be absent (note: Go drops "" too)
}
