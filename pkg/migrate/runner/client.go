package runner

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// ClusterClient is the subset of cluster operations the runner needs.
// The concrete implementation is the dynamic client from
// k8s.io/client-go; tests use a fake.
type ClusterClient interface {
	// List paginates a resource and yields each item to fn. It must
	// stop early if fn returns an error.
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, fn func(*unstructured.Unstructured) error) error

	// ServerSideApply applies obj to the cluster with the given field
	// manager. It is idempotent: re-applying the same object with the
	// same field manager is a no-op. namespaced selects between the
	// namespaced and cluster-scoped resource endpoints.
	ServerSideApply(ctx context.Context, gvr schema.GroupVersionResource, namespaced bool, obj *unstructured.Unstructured, fieldManager string) error
}

// dynamicClusterClient is the production ClusterClient backed by
// k8s.io/client-go/dynamic.
type dynamicClusterClient struct {
	dyn dynamic.Interface
}

// NewDynamicClusterClient adapts a dynamic.Interface to ClusterClient.
func NewDynamicClusterClient(dyn dynamic.Interface) ClusterClient {
	return &dynamicClusterClient{dyn: dyn}
}

const listPageSize = 500

func (c *dynamicClusterClient) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, fn func(*unstructured.Unstructured) error) error {
	var continueToken string
	for {
		opts := listOpts(continueToken)
		var (
			list *unstructured.UnstructuredList
			err  error
		)
		if namespace == "" {
			list, err = c.dyn.Resource(gvr).List(ctx, opts)
		} else {
			list, err = c.dyn.Resource(gvr).Namespace(namespace).List(ctx, opts)
		}
		if err != nil {
			return err
		}
		for i := range list.Items {
			item := list.Items[i]
			if err := fn(&item); err != nil {
				return err
			}
		}
		continueToken = list.GetContinue()
		if continueToken == "" {
			return nil
		}
	}
}

func (c *dynamicClusterClient) ServerSideApply(ctx context.Context, gvr schema.GroupVersionResource, namespaced bool, obj *unstructured.Unstructured, fieldManager string) error {
	data, err := obj.MarshalJSON()
	if err != nil {
		return err
	}
	opts := applyOpts(fieldManager)
	if namespaced {
		_, err = c.dyn.Resource(gvr).Namespace(obj.GetNamespace()).Patch(ctx, obj.GetName(), types.ApplyPatchType, data, opts)
	} else {
		_, err = c.dyn.Resource(gvr).Patch(ctx, obj.GetName(), types.ApplyPatchType, data, opts)
	}
	return err
}
