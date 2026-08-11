package k8s

import (
	"context"
	"testing"
	"watchdog-agent/internal/config"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIsExcluded(t *testing.T) {
	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			ExcludedNamespaces: []string{"kube-system"},
		},
	}
	client := &Client{Config: cfg}

	tests := []struct {
		name        string
		namespace   string
		annotations map[string]string
		want        bool
	}{
		{
			name:      "excluded namespace",
			namespace: "kube-system",
			want:      true,
		},
		{
			name:      "not excluded namespace",
			namespace: "default",
			want:      false,
		},
		{
			name:      "excluded by annotation",
			namespace: "default",
			annotations: map[string]string{
				"watchdog.finops.io/exclude": "true",
			},
			want: true,
		},
		{
			name:      "false annotation",
			namespace: "default",
			annotations: map[string]string{
				"watchdog.finops.io/exclude": "false",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.IsExcluded(tt.namespace, tt.annotations)
			if got != tt.want {
				t.Errorf("IsExcluded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyWorkload(t *testing.T) {
	client := &Client{}
	dep := &appsv1.Deployment{}
	if got := client.ClassifyWorkload(dep); got != WorkloadTypeDeployment {
		t.Errorf("ClassifyWorkload() = %v, want %v", got, WorkloadTypeDeployment)
	}
}

func TestGetDeployments(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dep",
			Namespace: "default",
		},
	})

	client := &Client{ClientSet: fakeClient}

	deps, err := client.GetDeployments(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps.Items) != 1 {
		t.Errorf("expected 1 deployment, got %d", len(deps.Items))
	}
}

func TestGetNodes(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	})

	client := &Client{ClientSet: fakeClient}

	nodes, err := client.GetNodes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes.Items) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes.Items))
	}
}

func TestGetNamespaces(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
		},
	})

	client := &Client{ClientSet: fakeClient}

	ns, err := client.GetNamespaces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ns.Items) != 1 {
		t.Errorf("expected 1 namespace, got %d", len(ns.Items))
	}
}
