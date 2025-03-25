// Package v1beta1 contains the input type for this Function
// +kubebuilder:object:generate=true
// +groupName=azresourcegraph.fn.crossplane.io
// +versionName=v1alpha1
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This isn't a custom resource, in the sense that we never install its CRD.
// It is a KRM-like object, so we generate a CRD to describe its schema.

// TODO: Add your input type here! It doesn't need to be called 'Input', you can
// rename it to anything you like.

// Input can be used to provide input to this Function.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=crossplane
type Input struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Query to Azure Resource Graph API
	// +optional
	Query string `json:"query,omitempty"`

	// Reference to retrieve the query string (e.g., from status or context)
	// Overrides Query field if used
	// +optional
	QueryRef *string `json:"queryRef,omitempty"`

	// Azure management groups against which to execute the query. Example: [ 'mg1', 'mg2' ]
	// +optional
	ManagementGroups []*string `json:"managementGroups,omitempty"`

	// Azure subscriptions against which to execute the query. Example: [ 'sub1','sub2' ]
	// +optional
	Subscriptions []*string `json:"subscriptions,omitempty"`

	// Reference to retrieve the subscriptions (e.g., from status or context)
	// Overrides Subscriptions field if used
	// +optional
	SubscriptionsRef *string `json:"subscriptionsRef,omitempty"`

	// Target where to store the Query Result
	Target string `json:"target"`

	// SkipQueryWhenTargetHasData controls whether to skip the query when the target already has data
	// Default is false to ensure continuous reconciliation
	// +optional
	SkipQueryWhenTargetHasData *bool `json:"skipQueryWhenTargetHasData,omitempty"`
}
