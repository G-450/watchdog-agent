package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Client handles communication with the Kubernetes API to inspect workloads.
type Client struct {
	ClientSet *kubernetes.Clientset
}

// NewClient initializes a new Kubernetes client using the in-cluster config.
func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		ClientSet: clientset,
	}, nil
}
