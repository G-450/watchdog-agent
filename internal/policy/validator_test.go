package policy

import (
	"testing"
	"time"

	"watchdog-agent/internal/model"
)

func TestLocalValidator_Validate(t *testing.T) {
	validator := NewLocalValidator()

	tests := []struct {
		name      string
		rec       *model.Recommendation
		expectErr bool
	}{
		{
			name: "Valid Recommendation",
			rec: &model.Recommendation{
				Target:        "default/my-app",
				CurrentState:  `{"replicas": 5, "cpu_requests": 1.0}`,
				ProposedState: `{"replicas": 3, "cpu_requests": 0.8}`,
				Timestamp:     time.Now(),
			},
			expectErr: false,
		},
		{
			name: "Excluded Namespace",
			rec: &model.Recommendation{
				Target:        "kube-system/coredns",
				CurrentState:  `{"replicas": 2}`,
				ProposedState: `{"replicas": 1}`,
				Timestamp:     time.Now(),
			},
			expectErr: true,
		},
		{
			name: "Below Min Replicas",
			rec: &model.Recommendation{
				Target:        "default/my-app",
				CurrentState:  `{"replicas": 2}`,
				ProposedState: `{"replicas": 1}`,
				Timestamp:     time.Now(),
			},
			expectErr: true,
		},
		{
			name: "Exceeds Max Step-Down Percent",
			rec: &model.Recommendation{
				Target:        "default/my-app",
				CurrentState:  `{"cpu_requests": 1.0}`,
				ProposedState: `{"cpu_requests": 0.5}`,
				Timestamp:     time.Now(),
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.rec)
			if (err != nil) != tt.expectErr {
				t.Errorf("Validate() error = %v, expectErr %v", err, tt.expectErr)
			}
			if tt.expectErr && tt.rec.Status != "Rejected" {
				t.Errorf("Expected status Rejected, got %s", tt.rec.Status)
			}
			if !tt.expectErr {
				if tt.rec.Status != "Approved" {
					t.Errorf("Expected status Approved, got %s", tt.rec.Status)
				}
				if len(tt.rec.RuleTrace) == 0 || tt.rec.RuleTrace[len(tt.rec.RuleTrace)-1] != "LocalPolicyEvaluated:Approved" {
					t.Errorf("Expected RuleTrace to contain LocalPolicyEvaluated:Approved, got %v", tt.rec.RuleTrace)
				}
			}
		})
	}
}

func TestOPAPolicyValidator(t *testing.T) {
	opa := NewOPAPolicyValidator("")

	lowConfRec := &model.Recommendation{
		Target:          "default/low-conf",
		ConfidenceScore: 0.40,
		ProposedState:   `{"cpu_requests": 0.5}`,
	}
	if err := opa.Validate(lowConfRec); err == nil {
		t.Errorf("Expected OPA validator to reject low confidence score")
	}

	prodRec := &model.Recommendation{
		Target:          "prod-checkout/service",
		ConfidenceScore: 0.85,
		ProposedState:   `{"replicas": 2}`,
	}
	if err := opa.Validate(prodRec); err == nil {
		t.Errorf("Expected OPA validator to reject prod workload with < 3 replicas")
	}

	validProdRec := &model.Recommendation{
		Target:          "prod-checkout/service",
		ConfidenceScore: 0.85,
		ProposedState:   `{"replicas": 4}`,
	}
	if err := opa.Validate(validProdRec); err != nil {
		t.Errorf("Expected valid prod recommendation to pass OPA, got error: %v", err)
	}

	// Non-production workloads whose names contain or start with "prod" should NOT be flagged as production
	nonProdCases := []string{
		"dev/product-catalog",
		"staging/producer-service",
	}
	for _, target := range nonProdCases {
		rec := &model.Recommendation{
			Target:          target,
			ConfidenceScore: 0.85,
			ProposedState:   `{"replicas": 1}`, // Replicas < 3 is allowed outside production
		}
		if err := opa.Validate(rec); err != nil {
			t.Errorf("Expected non-prod workload %s to not be flagged as prod, got error: %v", target, err)
		}
	}

	// Production namespace with / delimiter should be flagged as production
	prodNamespaceRec := &model.Recommendation{
		Target:          "prod/payment-service",
		ConfidenceScore: 0.85,
		ProposedState:   `{"replicas": 2}`,
	}
	if err := opa.Validate(prodNamespaceRec); err == nil {
		t.Errorf("Expected prod/payment-service with < 3 replicas to be rejected by OPA")
	}
}

func TestKyvernoPolicyValidator(t *testing.T) {
	kyverno := NewKyvernoPolicyValidator()

	tooLowCpuRec := &model.Recommendation{
		Target:        "default/micro-service",
		ProposedState: `{"cpu_requests": 0.02}`,
	}
	if err := kyverno.Validate(tooLowCpuRec); err == nil {
		t.Errorf("Expected Kyverno to reject cpu_requests below cluster limit")
	}

	validCpuRec := &model.Recommendation{
		Target:        "default/micro-service",
		ProposedState: `{"cpu_requests": 0.25}`,
	}
	if err := kyverno.Validate(validCpuRec); err != nil {
		t.Errorf("Expected valid recommendation to pass Kyverno, got error: %v", err)
	}
}

func TestCompositeValidator(t *testing.T) {
	local := NewLocalValidator()
	opa := NewOPAPolicyValidator("")
	kyverno := NewKyvernoPolicyValidator()

	composite := NewCompositeValidator(local, opa, kyverno)

	validRec := &model.Recommendation{
		Target:          "default/ecommerce-app",
		ConfidenceScore: 0.88,
		CurrentState:    `{"cpu_requests": 1.0, "replicas": 3}`,
		ProposedState:   `{"cpu_requests": 0.8, "replicas": 3}`,
		Timestamp:       time.Now(),
	}

	if err := composite.Validate(validRec); err != nil {
		t.Errorf("Expected valid recommendation to pass all composite validators, got: %v", err)
	}
	if validRec.Status != "Approved" {
		t.Errorf("Expected status Approved, got %s", validRec.Status)
	}
	if validRec.RejectionReason != "" {
		t.Errorf("Expected empty RejectionReason, got %s", validRec.RejectionReason)
	}

	// Verify RuleTrace is populated in order across all validators
	expectedTraces := []string{
		"LocalPolicyEvaluated:Approved",
		"OPAPolicyEvaluated:Approved",
		"KyvernoPolicyEvaluated:Approved",
	}
	if len(validRec.RuleTrace) != len(expectedTraces) {
		t.Errorf("Expected %d RuleTrace entries, got %d: %v", len(expectedTraces), len(validRec.RuleTrace), validRec.RuleTrace)
	} else {
		for i, expected := range expectedTraces {
			if validRec.RuleTrace[i] != expected {
				t.Errorf("Expected RuleTrace[%d] = %q, got %q", i, expected, validRec.RuleTrace[i])
			}
		}
	}

	// Should reject if LocalValidator fails (e.g. excluded namespace)
	localFailRec := &model.Recommendation{
		Target:          "kube-system/ecommerce-app",
		ConfidenceScore: 0.88,
		CurrentState:    `{"cpu_requests": 1.0, "replicas": 3}`,
		ProposedState:   `{"cpu_requests": 0.8, "replicas": 3}`,
		Timestamp:       time.Now(),
	}
	if err := composite.Validate(localFailRec); err == nil {
		t.Errorf("Expected composite validator to fail for kube-system")
	}
	if localFailRec.Status != "Rejected" {
		t.Errorf("Expected status Rejected for LocalValidator failure, got %s", localFailRec.Status)
	}

	// Should halt and reject if OPAPolicyValidator fails (e.g. low confidence score)
	opaFailRec := &model.Recommendation{
		Target:          "default/ecommerce-app",
		ConfidenceScore: 0.40, // Below OPA threshold 0.60
		CurrentState:    `{"cpu_requests": 1.0, "replicas": 3}`,
		ProposedState:   `{"cpu_requests": 0.8, "replicas": 3}`,
		Timestamp:       time.Now(),
	}
	if err := composite.Validate(opaFailRec); err == nil {
		t.Errorf("Expected composite validator to fail when OPA validator rejects low confidence")
	}
	if opaFailRec.Status != "Rejected" {
		t.Errorf("Expected status Rejected for OPA failure, got %s", opaFailRec.Status)
	}
	// Verify chain halted: Kyverno should not have evaluated or added to RuleTrace
	for _, trace := range opaFailRec.RuleTrace {
		if trace == "KyvernoPolicyEvaluated:Approved" {
			t.Errorf("Kyverno should not have evaluated after OPA failure")
		}
	}

	// Should halt and reject if KyvernoPolicyValidator fails (e.g. CPU request below cluster limit)
	kyvernoFailRec := &model.Recommendation{
		Target:          "default/ecommerce-app",
		ConfidenceScore: 0.88,
		CurrentState:    `{"cpu_requests": 1.0, "replicas": 3}`,
		ProposedState:   `{"cpu_requests": 0.02, "replicas": 3}`, // Below Kyverno 0.05 limit
		Timestamp:       time.Now(),
	}
	if err := composite.Validate(kyvernoFailRec); err == nil {
		t.Errorf("Expected composite validator to fail when Kyverno validator rejects low CPU request")
	}
	if kyvernoFailRec.Status != "Rejected" {
		t.Errorf("Expected status Rejected for Kyverno failure, got %s", kyvernoFailRec.Status)
	}

	// Verify CompositeValidator acts as the final authority on approval status even if LocalValidator is absent
	compositeWithoutLocal := NewCompositeValidator(opa, kyverno)
	noLocalRec := &model.Recommendation{
		Target:          "default/ecommerce-app",
		ConfidenceScore: 0.88,
		ProposedState:   `{"cpu_requests": 0.8, "replicas": 3}`,
		Timestamp:       time.Now(),
	}
	if err := compositeWithoutLocal.Validate(noLocalRec); err != nil {
		t.Errorf("Expected recommendation to pass composite without LocalValidator, got: %v", err)
	}
	if noLocalRec.Status != "Approved" {
		t.Errorf("Expected CompositeValidator to explicitly set Approved status, got %s", noLocalRec.Status)
	}
	if noLocalRec.RejectionReason != "" {
		t.Errorf("Expected empty RejectionReason, got %s", noLocalRec.RejectionReason)
	}
}
