package gitops

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

const bytesPerMi = 1024 * 1024

// resourceState is the subset of a recommendation's current/proposed state the generator acts on.
// Pointer fields distinguish "absent" from zero.
type resourceState struct {
	CPURequests *float64 `json:"cpu_requests"`
	MemRequests *float64 `json:"mem_requests"`
	Replicas    *int32   `json:"replicas"`
}

// ChangeSet holds the manifest values a recommendation asks for, already formatted as
// Kubernetes quantities. It is also the PR body marker, so its JSON form must be stable.
type ChangeSet struct {
	CPU      string `json:"cpu,omitempty"`
	Memory   string `json:"memory,omitempty"`
	Replicas *int32 `json:"replicas,omitempty"`
}

// IsEmpty reports whether the change set requests no change.
func (c ChangeSet) IsEmpty() bool {
	return c.CPU == "" && c.Memory == "" && c.Replicas == nil
}

// marker returns the JSON form embedded in PR bodies.
func (c ChangeSet) marker() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode change set: %w", err)
	}
	return string(b), nil
}

// merge adds the fields of other that c does not set yet and reports fields both set differently.
func (c *ChangeSet) merge(other ChangeSet) (conflicts []string) {
	if other.CPU != "" {
		if c.CPU == "" {
			c.CPU = other.CPU
		} else if c.CPU != other.CPU {
			conflicts = append(conflicts, "cpu")
		}
	}
	if other.Memory != "" {
		if c.Memory == "" {
			c.Memory = other.Memory
		} else if c.Memory != other.Memory {
			conflicts = append(conflicts, "memory")
		}
	}
	if other.Replicas != nil {
		if c.Replicas == nil {
			c.Replicas = other.Replicas
		} else if *c.Replicas != *other.Replicas {
			conflicts = append(conflicts, "replicas")
		}
	}
	return conflicts
}

// diffStates builds the change set from fields present in proposed whose formatted value
// differs from current. The AI service repeats unchanged fields, so presence alone is not a change.
func diffStates(currentJSON, proposedJSON string) (ChangeSet, error) {
	var current, proposed resourceState
	if currentJSON != "" {
		if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
			return ChangeSet{}, fmt.Errorf("parse current_state: %w", err)
		}
	}
	if err := json.Unmarshal([]byte(proposedJSON), &proposed); err != nil {
		return ChangeSet{}, fmt.Errorf("parse proposed_state: %w", err)
	}

	var cs ChangeSet
	if proposed.CPURequests != nil {
		if *proposed.CPURequests <= 0 || math.IsNaN(*proposed.CPURequests) || math.IsInf(*proposed.CPURequests, 0) {
			return ChangeSet{}, fmt.Errorf("proposed cpu_requests %v is not a positive number", *proposed.CPURequests)
		}
		want := formatCPU(*proposed.CPURequests)
		if current.CPURequests == nil || formatCPU(*current.CPURequests) != want {
			cs.CPU = want
		}
	}
	if proposed.MemRequests != nil {
		if *proposed.MemRequests <= 0 || math.IsNaN(*proposed.MemRequests) || math.IsInf(*proposed.MemRequests, 0) {
			return ChangeSet{}, fmt.Errorf("proposed mem_requests %v is not a positive number", *proposed.MemRequests)
		}
		want := formatMemory(*proposed.MemRequests)
		if current.MemRequests == nil || formatMemory(*current.MemRequests) != want {
			cs.Memory = want
		}
	}
	if proposed.Replicas != nil {
		if *proposed.Replicas < 0 {
			return ChangeSet{}, fmt.Errorf("proposed replicas %d is negative", *proposed.Replicas)
		}
		if current.Replicas == nil || *current.Replicas != *proposed.Replicas {
			r := *proposed.Replicas
			cs.Replicas = &r
		}
	}
	return cs, nil
}

// formatCPU renders cores as a millicore quantity, e.g. 0.35 -> "350m".
func formatCPU(cores float64) string {
	return fmt.Sprintf("%dm", int64(math.Round(cores*1000)))
}

// formatMemory renders bytes as "<n>Mi" when exact, otherwise as an integer byte count.
func formatMemory(bytes float64) string {
	n := int64(math.Round(bytes))
	if n%bytesPerMi == 0 {
		return strconv.FormatInt(n/bytesPerMi, 10) + "Mi"
	}
	return strconv.FormatInt(n, 10)
}
