package upgradeaudit

import (
	"encoding/json"
	"time"
)

// Report captures the diff between two sets of CRD manifests.
type Report struct {
	OldPath     string                    `json:"oldPath"`
	NewPath     string                    `json:"newPath"`
	GeneratedAt time.Time                 `json:"generatedAt"`
	Summary     SummaryCounts             `json:"summary"`
	Services    map[string]*ServiceReport `json:"services"`
	Notes       []string                  `json:"notes,omitempty"`
}

// SummaryCounts provides quick totals for change types.
type SummaryCounts struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Modified int `json:"modified"`
}

// ServiceReport groups changes by high-level OCI service.
type ServiceReport struct {
	ServiceID        string               `json:"serviceId"`
	AddedResources   []ResourceRef        `json:"addedResources,omitempty"`
	RemovedResources []ResourceRef        `json:"removedResources,omitempty"`
	Modified         []FieldChangeSummary `json:"modified,omitempty"`
}

// ResourceRef identifies a CRD manifest within a service.
type ResourceRef struct {
	Resource     string   `json:"resource"`
	Scope        string   `json:"scope"`
	RelativePath string   `json:"relativePath"`
	Notes        []string `json:"notes,omitempty"`
}

// FieldChangeSummary highlights schema-level edits for a resource.
type FieldChangeSummary struct {
	Resource       string       `json:"resource"`
	Scope          string       `json:"scope"`
	RelativePath   string       `json:"relativePath"`
	AddedRequired  []string     `json:"addedRequiredFields,omitempty"`
	AddedOptional  []string     `json:"addedOptionalFields,omitempty"`
	RemovedFields  []string     `json:"removedFields,omitempty"`
	BecameRequired []string     `json:"becameRequired,omitempty"`
	BecameOptional []string     `json:"becameOptional,omitempty"`
	TypeChanges    []TypeChange `json:"typeChanges,omitempty"`
	Notes          []string     `json:"notes,omitempty"`
}

// TypeChange captures a type alteration for a field path.
type TypeChange struct {
	Field   string `json:"field"`
	OldType string `json:"oldType"`
	NewType string `json:"newType"`
}

// HasChanges reports whether any substantive schema diffs were detected.
func (f FieldChangeSummary) HasChanges() bool {
	return len(f.AddedRequired) > 0 ||
		len(f.AddedOptional) > 0 ||
		len(f.RemovedFields) > 0 ||
		len(f.BecameRequired) > 0 ||
		len(f.BecameOptional) > 0 ||
		len(f.TypeChanges) > 0
}

// Marshal pretty-prints the report for human review.
func (r *Report) Marshal(indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(r, "", "  ")
	}
	return json.Marshal(r)
}
