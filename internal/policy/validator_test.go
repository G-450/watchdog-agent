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
