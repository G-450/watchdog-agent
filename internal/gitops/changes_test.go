package gitops

import (
	"testing"
)

func TestDiffStates(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		proposed string
		want     ChangeSet
		wantErr  bool
	}{
		{
			name:     "real approved rec changes only cpu",
			current:  `{"cpu_requests": 0.5, "replicas": 1, "mem_requests": 314572800.0}`,
			proposed: `{"cpu_requests": 0.35, "replicas": 1, "mem_requests": 314572800.0}`,
			want:     ChangeSet{CPU: "350m"},
		},
		{
			name:     "replicas only",
			current:  `{"replicas": 3, "cpu_requests": 0.2}`,
			proposed: `{"replicas": 2, "cpu_requests": 0.2}`,
			want:     ChangeSet{Replicas: int32p(2)},
		},
		{
			name:     "memory change",
			current:  `{"mem_requests": 314572800.0}`,
			proposed: `{"mem_requests": 268435456.0}`,
			want:     ChangeSet{Memory: "256Mi"},
		},
		{
			name:     "field missing from current counts as a change",
			current:  `{}`,
			proposed: `{"cpu_requests": 0.1}`,
			want:     ChangeSet{CPU: "100m"},
		},
		{
			name:     "empty current",
			proposed: `{"cpu_requests": 0.1}`,
			want:     ChangeSet{CPU: "100m"},
		},
		{
			name:     "difference below a millicore is no change",
			current:  `{"cpu_requests": 0.35}`,
			proposed: `{"cpu_requests": 0.3501}`,
		},
		{
			name:     "field absent from proposed is ignored",
			current:  `{"cpu_requests": 0.5, "replicas": 2}`,
			proposed: `{"replicas": 2}`,
		},
		{name: "invalid proposed", current: `{}`, proposed: `nope`, wantErr: true},
		{name: "invalid current", current: `nope`, proposed: `{}`, wantErr: true},
		{name: "zero cpu", current: `{}`, proposed: `{"cpu_requests": 0}`, wantErr: true},
		{name: "negative memory", current: `{}`, proposed: `{"mem_requests": -1}`, wantErr: true},
		{name: "negative replicas", current: `{}`, proposed: `{"replicas": -1}`, wantErr: true},
		{name: "fractional replicas", current: `{}`, proposed: `{"replicas": 1.5}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := diffStates(tt.current, tt.proposed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			gm, _ := got.marker()
			wm, _ := tt.want.marker()
			if gm != wm {
				t.Errorf("got %s, want %s", gm, wm)
			}
		})
	}
}

func TestFormatCPU(t *testing.T) {
	tests := []struct {
		cores float64
		want  string
	}{
		{0.35, "350m"},
		{0.5, "500m"},
		{1.005, "1005m"},
		{2, "2000m"},
		{0.0004, "0m"},
		{0.0306, "31m"},
	}
	for _, tt := range tests {
		if got := formatCPU(tt.cores); got != tt.want {
			t.Errorf("formatCPU(%v) = %q, want %q", tt.cores, got, tt.want)
		}
	}
}

func TestFormatMemory(t *testing.T) {
	tests := []struct {
		bytes float64
		want  string
	}{
		{314572800.0, "300Mi"},
		{1048576, "1Mi"},
		{1073741824, "1024Mi"},
		{115343360.0, "110Mi"},
		{100000000, "100000000"},
		{1048577, "1048577"},
		{314572800.4, "300Mi"},
	}
	for _, tt := range tests {
		if got := formatMemory(tt.bytes); got != tt.want {
			t.Errorf("formatMemory(%v) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestChangeSetMerge(t *testing.T) {
	a := ChangeSet{CPU: "350m"}
	if c := a.merge(ChangeSet{Replicas: int32p(2)}); len(c) != 0 {
		t.Errorf("unexpected conflicts %v", c)
	}
	if a.CPU != "350m" || a.Replicas == nil || *a.Replicas != 2 {
		t.Errorf("merge result %+v", a)
	}
	if c := a.merge(ChangeSet{CPU: "400m", Replicas: int32p(2)}); len(c) != 1 || c[0] != "cpu" {
		t.Errorf("conflicts = %v, want [cpu]", c)
	}
	if a.CPU != "350m" {
		t.Error("first value must win")
	}
}
