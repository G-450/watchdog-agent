package k8s

import (
	"context"
	"os"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client handles communication with the Kubernetes API to inspect workloads.
type Client struct {
	ClientSet *kubernetes.Clientset
}

// NewClient initializes a new Kubernetes client.
// It attempts to use in-cluster config first (when running as a pod),
// and falls back to out-of-cluster config (kubeconfig) for local testing.
func NewClient() (*Client, error) {
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
	}, nil
}

// GetDeployments retrieves all deployments in a given namespace.
// Pass an empty string "" to list deployments across all namespaces.
func (c *Client) GetDeployments(ctx context.Context, namespace string) (*appsv1.DeploymentList, error) {
	return c.ClientSet.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
}
