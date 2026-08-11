package k8s

import (
	"context"
	"os"
	"path/filepath"

	"watchdog-agent/internal/config"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// WorkloadType represents the type of workload.
type WorkloadType string

const (
	WorkloadTypeDeployment  WorkloadType = "Deployment"
	WorkloadTypeStatefulSet WorkloadType = "StatefulSet"
	WorkloadTypeDaemonSet   WorkloadType = "DaemonSet"
	WorkloadTypeJob         WorkloadType = "Job"
	WorkloadTypeCronJob     WorkloadType = "CronJob"
	WorkloadTypeUnknown     WorkloadType = "Unknown"
)

// Client handles communication with the Kubernetes API to inspect workloads.
type Client struct {
	ClientSet kubernetes.Interface
	Config    *config.Config
}

// NewClient initializes a new Kubernetes client.
// It attempts to use in-cluster config first (when running as a pod),
// and falls back to out-of-cluster config (kubeconfig) for local testing.
func NewClient(cfg *config.Config) (*Client, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to local kubeconfig
		home := homedir.HomeDir()
		kubeconfig := filepath.Join(home, ".kube", "config")
		if envVar := os.Getenv("KUBECONFIG"); envVar != "" {
			kubeconfig = envVar
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		ClientSet: clientset,
		Config:    cfg,
	}, nil
}

// GetDeployments retrieves all deployments in a given namespace.
// Pass an empty string "" to list deployments across all namespaces.
func (c *Client) GetDeployments(ctx context.Context, namespace string) (*appsv1.DeploymentList, error) {
	return c.ClientSet.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
}

// GetServices retrieves all services in a given namespace.
func (c *Client) GetServices(ctx context.Context, namespace string) (*corev1.ServiceList, error) {
	return c.ClientSet.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
}

// GetNodes retrieves all nodes in the cluster.
func (c *Client) GetNodes(ctx context.Context) (*corev1.NodeList, error) {
	return c.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
}

// GetNamespaces retrieves all namespaces in the cluster.
func (c *Client) GetNamespaces(ctx context.Context) (*corev1.NamespaceList, error) {
	return c.ClientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
}

// GetPods retrieves all pods in a given namespace.
func (c *Client) GetPods(ctx context.Context, namespace string) (*corev1.PodList, error) {
	return c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
}

// GetHPAs retrieves all HorizontalPodAutoscalers in a given namespace.
func (c *Client) GetHPAs(ctx context.Context, namespace string) (*autoscalingv2.HorizontalPodAutoscalerList, error) {
	return c.ClientSet.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
}

// ClassifyWorkload returns the type of the workload based on its owner references.
// For now, this just assumes Deployment since we only deal with Deployment structs directly,
// but it is a stub for future broader classification.
func (c *Client) ClassifyWorkload(deployment *appsv1.Deployment) WorkloadType {
	// A Deployment is always a Deployment.
	return WorkloadTypeDeployment
}

// IsExcluded checks if a workload in the given namespace should be excluded.
func (c *Client) IsExcluded(namespace string, annotations map[string]string) bool {
	// Check namespace exclusions
	for _, excludedNs := range c.Config.Kubernetes.ExcludedNamespaces {
		if namespace == excludedNs {
			return true
		}
	}

	// Check annotation exclusion
	if val, ok := annotations["watchdog.finops.io/exclude"]; ok && val == "true" {
		return true
	}

	return false
}
