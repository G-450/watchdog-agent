package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	"watchdog-agent/internal/model"
)

// Validator defines the interface for recommendation validation against policies.
type Validator interface {
	Validate(rec *model.Recommendation) error
}

// LocalValidator implements Validator using predefined rules.
type LocalValidator struct {
	MinReplicas        int
	MaxStepDownPercent float64 // 0.0 to 1.0
	ExcludedNamespaces []string
}

// NewLocalValidator creates a new local policy validator.
func NewLocalValidator() *LocalValidator {
	return &LocalValidator{
		MinReplicas:        2,
		MaxStepDownPercent: 0.30,
		ExcludedNamespaces: []string{"kube-system", "monitoring", "watchdog"},
	}
}

// Validate checks if the recommendation adheres to organizational policies.
func (v *LocalValidator) Validate(rec *model.Recommendation) error {
	// 1. Excluded Namespaces
	for _, ns := range v.ExcludedNamespaces {
		if strings.Contains(rec.Target, "/namespace/"+ns+"/") || strings.Contains(rec.Target, ns) {
			return v.reject(rec, fmt.Sprintf("namespace %s is excluded from automated changes", ns))
		}
	}

	var currentState, proposedState map[string]interface{}
	errC := json.Unmarshal([]byte(rec.CurrentState), &currentState)
	errP := json.Unmarshal([]byte(rec.ProposedState), &proposedState)

	if errC == nil && errP == nil {
		// Min Replicas check
		if proposedReps, ok := proposedState["replicas"].(float64); ok {
			if int(proposedReps) < v.MinReplicas {
				return v.reject(rec, fmt.Sprintf("proposed replicas (%d) is below minimum allowed (%d)", int(proposedReps), v.MinReplicas))
			}
		}

		// Max Step-Down Percent check for CPU requests
		if currentCPU, okC := currentState["cpu_requests"].(float64); okC {
			if proposedCPU, okP := proposedState["cpu_requests"].(float64); okP {
				if proposedCPU < currentCPU {
					stepDown := (currentCPU - proposedCPU) / currentCPU
					if stepDown > v.MaxStepDownPercent {
						return v.reject(rec, fmt.Sprintf("proposed CPU step-down (%.1f%%) exceeds maximum allowed (%.1f%%)", stepDown*100, v.MaxStepDownPercent*100))
					}
				}
			}
		}
	}

	rec.Status = "Approved"
	rec.RejectionReason = ""
	return nil
}

func (v *LocalValidator) reject(rec *model.Recommendation, reason string) error {
	rec.Status = "Rejected"
	rec.RejectionReason = reason
	return fmt.Errorf("policy violation: %s", reason)
}
