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
			if !tt.expectErr && tt.rec.Status != "Approved" {
				t.Errorf("Expected status Approved, got %s", tt.rec.Status)
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

	// Should reject if any sub-validator fails
	invalidRec := &model.Recommendation{
		Target:          "kube-system/ecommerce-app",
		ConfidenceScore: 0.88,
		CurrentState:    `{"cpu_requests": 1.0, "replicas": 3}`,
		ProposedState:   `{"cpu_requests": 0.8, "replicas": 3}`,
		Timestamp:       time.Now(),
	}
	if err := composite.Validate(invalidRec); err == nil {
		t.Errorf("Expected composite validator to fail for kube-system")
	}
}
