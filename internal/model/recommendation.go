package model

import "time"

// Recommendation represents an AI-generated optimization recommendation.
type Recommendation struct {
	ID                 int64     `json:"id,omitempty"`
	Target             string    `json:"target"`
	CurrentState       string    `json:"current_state"`
	ProposedState      string    `json:"proposed_state"`
	ExpectedSavings    float64   `json:"expected_savings"`
	ConfidenceScore    float64   `json:"confidence_score"`
	SupportingEvidence string    `json:"supporting_evidence"`
	RuleTrace          []string  `json:"rule_trace"`
	Status             string    `json:"status"` // e.g., "Pending", "Approved", "Rejected"
	RejectionReason    string    `json:"rejection_reason,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
}
