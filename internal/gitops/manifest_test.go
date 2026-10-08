package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, workload, file string) manifestFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "manifests", "workloads", "default", workload, file))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Fixtures may be checked out with CRLF on Windows; the infra repo stores LF.
	data = []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
	return manifestFile{Path: "workloads/default/" + workload + "/" + file, Data: data}
}

// lineDiff returns the lines that differ between a and b, assuming one contiguous change.
func lineDiff(a, b string) (removed, added []string) {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	i := 0
	for i < len(al) && i < len(bl) && al[i] == bl[i] {
		i++
	}
	ja, jb := len(al)-1, len(bl)-1
	for ja >= i && jb >= i && al[ja] == bl[jb] {
		ja--
		jb--
	}
	return al[i : ja+1], bl[i : jb+1]
}

func int32p(v int32) *int32 { return &v }

func TestPatchWorkload(t *testing.T) {
	yolo := readFixture(t, "yolo-detector", "yolo.yaml")
	calc := readFixture(t, "flask-calc", "calc-deployment.yaml")
	sidecar := readFixture(t, "sidecar-demo", "deployment.yaml")

	tests := []struct {
		name        string
		file        manifestFile
		workload    string
		cs          ChangeSet
		wantRemoved []string
		wantAdded   []string
		wantSkipped int
	}{
		{
			name:        "yolo cpu keeps quotes and comment",
			file:        yolo,
			workload:    "yolo-detector",
			cs:          ChangeSet{CPU: "350m"},
			wantRemoved: []string{`            cpu: "500m"      # The HPA will base its 70% target off this value`},
			wantAdded:   []string{`            cpu: "350m"      # The HPA will base its 70% target off this value`},
		},
		{
			name:        "yolo replicas skipped under HPA",
			file:        yolo,
			workload:    "yolo-detector",
			cs:          ChangeSet{Replicas: int32p(2)},
			wantSkipped: 1,
		},
		{
			name:        "yolo memory",
			file:        yolo,
			workload:    "yolo-detector",
			cs:          ChangeSet{Memory: "256Mi"},
			wantRemoved: []string{`            memory: "300Mi"`},
			wantAdded:   []string{`            memory: "256Mi"`},
		},
		{
			name:     "flask-calc creates resources block",
			file:     calc,
			workload: "flask-calc",
			cs:       ChangeSet{CPU: "350m"},
			wantAdded: []string{
				`        resources:`,
				`          requests:`,
				`            cpu: "350m"`,
			},
		},
		{
			name:     "flask-calc cpu and memory share one block",
			file:     calc,
			workload: "flask-calc",
			cs:       ChangeSet{CPU: "350m", Memory: "128Mi"},
			wantAdded: []string{
				`        resources:`,
				`          requests:`,
				`            cpu: "350m"`,
				`            memory: "128Mi"`,
			},
		},
		{
			name:        "flask-calc replicas",
			file:        calc,
			workload:    "flask-calc",
			cs:          ChangeSet{Replicas: int32p(1)},
			wantRemoved: []string{`  replicas: 2`},
			wantAdded:   []string{`  replicas: 1`},
		},
		{
			name:        "sidecar cpu skipped",
			file:        sidecar,
			workload:    "sidecar-demo",
			cs:          ChangeSet{CPU: "500m"},
			wantSkipped: 1,
		},
		{
			name:     "value already present",
			file:     yolo,
			workload: "yolo-detector",
			cs:       ChangeSet{CPU: "500m"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := patchWorkload([]manifestFile{tt.file}, tt.workload, tt.cs)
			if err != nil {
				t.Fatalf("patchWorkload: %v", err)
			}
			removed, added := lineDiff(string(tt.file.Data), string(res.Data))
			if strings.Join(removed, "\n") != strings.Join(tt.wantRemoved, "\n") ||
				strings.Join(added, "\n") != strings.Join(tt.wantAdded, "\n") {
				t.Errorf("diff mismatch\nremoved: %q\nadded:   %q", removed, added)
			}
			if len(res.Skipped) != tt.wantSkipped {
				t.Errorf("skipped = %v, want %d entries", res.Skipped, tt.wantSkipped)
			}
			if (len(res.Edits) == 0) != (len(tt.wantAdded) == 0) {
				t.Errorf("edits = %v", res.Edits)
			}
		})
	}
}

func TestPatchWorkload_PreservesCRLF(t *testing.T) {
	f := readFixture(t, "flask-calc", "calc-deployment.yaml")
	f.Data = []byte(strings.ReplaceAll(string(f.Data), "\n", "\r\n"))
	res, err := patchWorkload([]manifestFile{f}, "flask-calc", ChangeSet{CPU: "100m"})
	if err != nil {
		t.Fatalf("patchWorkload: %v", err)
	}
	if strings.Count(string(res.Data), "\n") != strings.Count(string(res.Data), "\r\n") {
		t.Error("patched file mixes line endings")
	}
}

func TestPatchWorkload_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"no deployment", "apiVersion: v1\nkind: Service\nmetadata:\n  name: web\n"},
		{"flow resources", "kind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n      - name: a\n        resources: {}\n"},
		{"invalid yaml", "kind: Deployment\n  bad: [\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := patchWorkload([]manifestFile{{Path: "x.yaml", Data: []byte(tt.data)}}, "web", ChangeSet{CPU: "100m"})
			if err == nil {
				t.Error("expected an error")
			}
		})
	}
}
