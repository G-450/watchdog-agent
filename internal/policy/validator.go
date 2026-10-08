package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

// stepDownTolerance absorbs rounding in proposals that sit exactly on the step-down limit.
const stepDownTolerance = 1e-6

// NewFromConfig builds the composite validator the agent runs, using configured guardrails.
func NewFromConfig(cfg config.PolicyConfig) *CompositeValidator {
	local := NewLocalValidator()
	local.MinReplicas = cfg.MinReplicas
	local.MaxStepDownPercent = cfg.MaxStepDownPercent
	local.ExcludedNamespaces = cfg.ExcludedNamespaces

	opa := NewOPAPolicyValidator("")
	opa.MinConfidenceScore = cfg.MinConfidence

	kyverno := NewKyvernoPolicyValidator()
	kyverno.MinCPURequest = cfg.MinCPURequest

	return NewCompositeValidator(local, opa, kyverno)
}

// replicasBelow reports whether a recommendation leaves fewer than floor replicas by removing some.
// Changes that keep the replica count (such as CPU rightsizing) never violate a replica floor.
// When the current count is unknown, any proposed count below the floor is treated as a violation.
func replicasBelow(rec *model.Recommendation, floor int) (int, bool) {
	var currentState, proposedState map[string]interface{}
	if err := json.Unmarshal([]byte(rec.ProposedState), &proposedState); err != nil {
		return 0, false
	}
	proposed, ok := proposedState["replicas"].(float64)
	if !ok || int(proposed) >= floor {
		return int(proposed), false
	}
	if err := json.Unmarshal([]byte(rec.CurrentState), &currentState); err == nil {
		if current, ok := currentState["replicas"].(float64); ok && proposed >= current {
			return int(proposed), false
		}
	}
	return int(proposed), true
}

// targetNamespace returns the namespace part of a "namespace/name" target.
func targetNamespace(target string) string {
	namespace, _, _ := strings.Cut(strings.Trim(target, "/"), "/")
	return namespace
}

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
	namespace := targetNamespace(rec.Target)
	for _, ns := range v.ExcludedNamespaces {
		if namespace == ns {
			return v.reject(rec, fmt.Sprintf("namespace %s is excluded from automated changes", ns))
		}
	}

	var currentState, proposedState map[string]interface{}
	errC := json.Unmarshal([]byte(rec.CurrentState), &currentState)
	errP := json.Unmarshal([]byte(rec.ProposedState), &proposedState)

	// Min Replicas check
	if proposed, below := replicasBelow(rec, v.MinReplicas); below {
		return v.reject(rec, fmt.Sprintf("proposed replicas (%d) is below minimum allowed (%d)", proposed, v.MinReplicas))
	}

	if errC == nil && errP == nil {

		// Max Step-Down Percent check for CPU requests
		if currentCPU, okC := currentState["cpu_requests"].(float64); okC {
			if proposedCPU, okP := proposedState["cpu_requests"].(float64); okP {
				if proposedCPU < currentCPU {
					stepDown := (currentCPU - proposedCPU) / currentCPU
					if stepDown > v.MaxStepDownPercent+stepDownTolerance {
						return v.reject(rec, fmt.Sprintf("proposed CPU step-down (%.1f%%) exceeds maximum allowed (%.1f%%)", stepDown*100, v.MaxStepDownPercent*100))
					}
				}
			}
		}
	}

	rec.Status = "Approved"
	rec.RejectionReason = ""
	rec.RuleTrace = append(rec.RuleTrace, "LocalPolicyEvaluated:Approved")
	return nil
}

func (v *LocalValidator) reject(rec *model.Recommendation, reason string) error {
	return reject(rec, "LocalPolicyEvaluated:Rejected", reason)
}

// reject marks a recommendation as rejected and records which policy stopped it.
func reject(rec *model.Recommendation, trace, reason string) error {
	rec.Status = "Rejected"
	rec.RejectionReason = reason
	rec.RuleTrace = append(rec.RuleTrace, trace)
	return fmt.Errorf("policy violation: %s", reason)
}

// isProductionWorkload determines whether a recommendation target corresponds to a production workload.
// It parses the target into namespace and workload components to avoid false positives on names like 'product-catalog'.
func isProductionWorkload(target string) bool {
	cleaned := strings.Trim(target, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 || parts[0] == "" {
		return false
	}

	ns := parts[0]
	if ns == "prod" || ns == "production" || strings.HasPrefix(ns, "prod-") || strings.HasPrefix(ns, "production-") {
		return true
	}

	if len(parts) > 1 {
		workload := parts[1]
		if workload == "prod" || workload == "production" || strings.HasPrefix(workload, "prod-") || strings.HasPrefix(workload, "production-") {
			return true
		}
	}

	return false
}

// OPAPolicyValidator is an enterprise stub simulating Open Policy Agent (OPA) / Rego evaluations.
type OPAPolicyValidator struct {
	Endpoint           string // TODO: Implement actual HTTP call to Endpoint
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
	// TODO: Implement actual HTTP call to Endpoint

	// Confidence score admission threshold
	if rec.ConfidenceScore < o.MinConfidenceScore {
		return reject(rec, "OPAPolicyEvaluated:Rejected", fmt.Sprintf("OPA policy violation: confidence score %.2f is below admission threshold %.2f", rec.ConfidenceScore, o.MinConfidenceScore))
	}

	// Production safeguarding Rego rule
	if isProductionWorkload(rec.Target) {
		if _, below := replicasBelow(rec, 3); below {
			return reject(rec, "OPAPolicyEvaluated:Rejected", "OPA policy violation (rego: prod_high_availability): production workloads must maintain at least 3 replicas")
		}
	}

	rec.RuleTrace = append(rec.RuleTrace, "OPAPolicyEvaluated:Approved")
	return nil
}

// KyvernoPolicyValidator is a stub simulating Kyverno Kubernetes cluster policy validation.
type KyvernoPolicyValidator struct {
	PolicyName         string
	EnforceLimitRanges bool
	MinCPURequest      float64 // cores
}

// NewKyvernoPolicyValidator creates a new Kyverno policy stub.
func NewKyvernoPolicyValidator() *KyvernoPolicyValidator {
	return &KyvernoPolicyValidator{
		PolicyName:         "watchdog-resource-quotas",
		EnforceLimitRanges: true,
		MinCPURequest:      0.05,
	}
}

// Validate checks recommendations against simulated Kyverno validation rules.
func (k *KyvernoPolicyValidator) Validate(rec *model.Recommendation) error {
	var proposedState map[string]interface{}
	if err := json.Unmarshal([]byte(rec.ProposedState), &proposedState); err == nil {
		if cpu, ok := proposedState["cpu_requests"].(float64); ok {
			if cpu < k.MinCPURequest {
				return reject(rec, "KyvernoPolicyEvaluated:Rejected", fmt.Sprintf("Kyverno policy violation: proposed CPU request is below cluster minimum limit (%dm)", int(k.MinCPURequest*1000)))
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
	rec.Status = "Approved"
	rec.RejectionReason = ""
	return nil
}
