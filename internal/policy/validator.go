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

// LocalValidator implements Validator using predefined static baseline rules.
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

// OPAPolicyValidator is an enterprise stub simulating Open Policy Agent (OPA) / Rego evaluations.
type OPAPolicyValidator struct {
	Endpoint           string
	MinConfidenceScore float64
	StrictFinOpsRules  bool
}

// NewOPAPolicyValidator creates a new stub for OPA Rego policy evaluation.
func NewOPAPolicyValidator(endpoint string) *OPAPolicyValidator {
	if endpoint == "" {
		endpoint = "http://opa-service.monitoring.svc:8181/v1/data/watchdog/allow"
	}
	return &OPAPolicyValidator{
		Endpoint:           endpoint,
		MinConfidenceScore: 0.60,
		StrictFinOpsRules:  true,
	}
}

// Validate evaluates the recommendation against simulated OPA Rego admission criteria.
func (o *OPAPolicyValidator) Validate(rec *model.Recommendation) error {
	// Confidence score admission threshold
	if rec.ConfidenceScore < o.MinConfidenceScore {
		rec.Status = "Rejected"
		rec.RejectionReason = fmt.Sprintf("OPA policy violation: confidence score %.2f is below admission threshold %.2f", rec.ConfidenceScore, o.MinConfidenceScore)
		return fmt.Errorf("%s", rec.RejectionReason)
	}

	// Production safeguarding Rego rule
	if strings.HasPrefix(rec.Target, "prod-") || strings.Contains(rec.Target, "/prod") {
		var proposedState map[string]interface{}
		if err := json.Unmarshal([]byte(rec.ProposedState), &proposedState); err == nil {
			if reps, ok := proposedState["replicas"].(float64); ok && int(reps) < 3 {
				rec.Status = "Rejected"
				rec.RejectionReason = "OPA policy violation (rego: prod_high_availability): production workloads must maintain at least 3 replicas"
				return fmt.Errorf("%s", rec.RejectionReason)
			}
		}
	}

	rec.RuleTrace = append(rec.RuleTrace, "OPAPolicyEvaluated:Approved")
	return nil
}

// KyvernoPolicyValidator is a stub simulating Kyverno Kubernetes cluster policy validation.
type KyvernoPolicyValidator struct {
	PolicyName         string
	EnforceLimitRanges bool
}

// NewKyvernoPolicyValidator creates a new Kyverno policy stub.
func NewKyvernoPolicyValidator() *KyvernoPolicyValidator {
	return &KyvernoPolicyValidator{
		PolicyName:         "watchdog-resource-quotas",
		EnforceLimitRanges: true,
	}
}

// Validate checks recommendations against simulated Kyverno validation rules.
func (k *KyvernoPolicyValidator) Validate(rec *model.Recommendation) error {
	var proposedState map[string]interface{}
	if err := json.Unmarshal([]byte(rec.ProposedState), &proposedState); err == nil {
		if cpu, ok := proposedState["cpu_requests"].(float64); ok {
			if cpu < 0.05 {
				rec.Status = "Rejected"
				rec.RejectionReason = "Kyverno policy violation: proposed CPU request is below cluster minimum limit (50m)"
				return fmt.Errorf("%s", rec.RejectionReason)
			}
		}
	}

	rec.RuleTrace = append(rec.RuleTrace, "KyvernoPolicyEvaluated:Approved")
	return nil
}

// CompositeValidator aggregates multiple validators and executes them sequentially.
type CompositeValidator struct {
	Validators []Validator
}

// NewCompositeValidator constructs a composite validator chaining multiple policies.
func NewCompositeValidator(validators ...Validator) *CompositeValidator {
	return &CompositeValidator{Validators: validators}
}

// Validate executes all configured validators. If any validator rejects the recommendation, it fails.
func (c *CompositeValidator) Validate(rec *model.Recommendation) error {
	for _, v := range c.Validators {
		if err := v.Validate(rec); err != nil {
			return err
		}
	}
	return nil
}
