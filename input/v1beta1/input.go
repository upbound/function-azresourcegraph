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

	// QueryIntervalMinutes specifies the minimum interval between queries in minutes
	// Used to prevent throttling and handle partial data scenarios
	// Default is 0 (no interval limiting)
	// +optional
	QueryIntervalMinutes *int `json:"queryIntervalMinutes,omitempty"`

	// Identity defines the type of identity used for authentication to the Microsoft Graph API.
	// +optional
	Identity *Identity `json:"identity,omitempty"`
}

// Identity defines the type of identity used for authentication to the Microsoft Graph API.
type Identity struct {
	// Type of credentials used to authenticate to the Microsoft Graph API.
	Type IdentityType `json:"type"`
}

const (
	// IdentityTypeAzureServicePrincipalCredentials defines default IdentityType which uses client id/client secret pair for authentication
	IdentityTypeAzureServicePrincipalCredentials IdentityType = "AzureServicePrincipalCredentials"
	// IdentityTypeAzureWorkloadIdentityCredentials defines default IdentityType which uses workload identity credentials for authentication
	IdentityTypeAzureWorkloadIdentityCredentials IdentityType = "AzureWorkloadIdentityCredentials"
)

// IdentityType controls type of credentials to use for authentication to the Microsoft Graph API.
// Supported values: AzureServicePrincipalCredentials;AzureWorkloadIdentityCredentials
type IdentityType string
