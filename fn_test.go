package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/upbound/function-azresourcegraph/input/v1beta1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

type MockAzureQuery struct {
	AzQueryFunc func(ctx context.Context, azureCreds map[string]string, in *v1beta1.Input) (armresourcegraph.ClientResourcesResponse, error)
}

func (m *MockAzureQuery) azQuery(ctx context.Context, azureCreds map[string]string, in *v1beta1.Input) (armresourcegraph.ClientResourcesResponse, error) {
	return m.AzQueryFunc(ctx, azureCreds, in)
}

func strPtr(s string) *string {
	return &s
}

func TestRunFunction(t *testing.T) {

	var (
		xr    = `{"apiVersion":"example.org/v1","kind":"XR","metadata":{"name":"cool-xr"},"spec":{"count":2}}`
		creds = &fnv1.CredentialData{
			Data: map[string][]byte{
				"credentials": []byte(`{
"clientId": "test-cliend-id",
"clientSecret": "test-client-secret",
"subscriptionId": "test-subscription-id",
"tenantId": "test-tenant-id"
}`),
			},
		}
	)

	type args struct {
		ctx context.Context
		req *fnv1.RunFunctionRequest
	}
	type want struct {
		rsp *fnv1.RunFunctionResponse
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ResponseIsReturned": {
			reason: "The Function should return a fatal result if no credentials were specified",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count"
					}`),
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "failed to get azure-creds credentials",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "",
								"kind": ""
							}`),
						},
					},
				},
			},
		},
		"ResponseIsReturnedWithOptionalManagementGroups": {
			reason: "The Function should accept optional managmenetGroups input",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"]
					}`),
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "failed to get azure-creds credentials",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "",
								"kind": ""
							}`),
						},
					},
				},
			},
		},
		"ShouldUpdateXRStatus": {
			reason: "The Function should update XR status",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldUpdateNestedFieldinXRStatus": {
			reason: "The Function should update nested field in XR status",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.nestedField.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult":
											{
												"resource": "mock-resource"
											}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldUpdateNestedComplexFieldinXRStatus": {
			reason: "The Function should update nested complex field with dots in XR status",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.[strange.nested.field.with.dots].azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"strange.nested.field.with.dots": {
										"azResourceGraphQueryResult":
											{
												"resource": "mock-resource"
											}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldKeepOtherFieldsInXRStatusDuringUpdate": {
			reason: "The Function should update nested field in XR status and keep the other status fields intact",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.nestedField.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"someField": "keepmearound"
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"someField": "keepmearound",
									"nestedField": {
										"azResourceGraphQueryResult":
											{
												"resource": "mock-resource"
											}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldKeepOtherFieldsFromPreviousPipelineStepInXRStatusDuringUpdate": {
			reason: "The Function should update nested field in XR status and keep the other status fields intact if they are coming from previous pipeline step",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.nestedField.azResourceGraphQueryResult"
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"someFieldDesired": "keepmearound"
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"someFieldDesired": "keepmearound",
									"nestedField": {
										"azResourceGraphQueryResult":
											{
												"resource": "mock-resource"
											}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldFailWithUnsupportedTarget": {
			reason: "The Function fail in case of unsupported value in Target Field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "notcool.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "Unrecognized target field: notcool.azResourceGraphQueryResult",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"ShouldUpdateContexField": {
			reason: "The Function should update Context Field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								}
						  }`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"ShouldUpdateNestedContexField": {
			reason: "The Function should update nested Context Field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.nestedField.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"nestedField": {
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								}
							}
						  }`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"ShouldUpdateEnvironmentContexField": {
			reason: "The Function should update environment Context Field that contains dots",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.[apiextensions.crossplane.io/environment].azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"apiextensions.crossplane.io/environment": {
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								}
							}
						  }`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"CanGetQueryFromContext": {
			reason: "The Function should be able to get Query from the Context field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"queryRef": "context.azResourceGraphQuery",
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "existing-data"
										}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
					Context: resource.MustStructJSON(
						`{
							"azResourceGraphQuery": "QueryFromContext"
						}`,
					),
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "QueryFromContext"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								},
							"azResourceGraphQuery": "QueryFromContext"
						}`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "existing-data"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldSkipQueryWhenNestedStatusTargetHasData": {
			reason: "The Function should skip query when nested status target already has data",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.nestedField.azResourceGraphQueryResult",
						"skipQueryWhenTargetHasData": true
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult": {
											"resource": "existing-data"
										}
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSkip",
							Message: strPtr("Target already has data, skipped query to avoid throttling"),
							Status:  fnv1.Status_STATUS_CONDITION_TRUE,
							Reason:  "SkippedQuery",
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult": {
											"resource": "existing-data"
										}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldSkipQueryWhenContextTargetHasData": {
			reason: "The Function should skip query when context target already has data",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.azResourceGraphQueryResult",
						"skipQueryWhenTargetHasData": true
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult": {
											"resource": "existing-data"
										}
									}
								}}`),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult": {
							"resource": "existing-data"
						}
					}`),
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSkip",
							Message: strPtr("Target already has data, skipped query to avoid throttling"),
							Status:  fnv1.Status_STATUS_CONDITION_TRUE,
							Reason:  "SkippedQuery",
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult": {
							"resource": "existing-data"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult": {
											"resource": "existing-data"
										}
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldSkipQueryWhenNestedContextTargetHasData": {
			reason: "The Function should skip query when nested context target already has data",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.nestedField.azResourceGraphQueryResult",
						"skipQueryWhenTargetHasData": true
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
					Context: resource.MustStructJSON(`{
						"nestedField": {
							"azResourceGraphQueryResult": {
								"resource": "existing-data"
							}
						}
					}`),
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSkip",
							Message: strPtr("Target already has data, skipped query to avoid throttling"),
							Status:  fnv1.Status_STATUS_CONDITION_TRUE,
							Reason:  "SkippedQuery",
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"nestedField": {
							"azResourceGraphQueryResult": {
								"resource": "existing-data"
							}
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenStatusTargetHasEmptyMap": {
			reason: "The Function should execute query when status target has empty map",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult": {}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenContextTargetHasEmptyMap": {
			reason: "The Function should execute query when context target has empty map",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult": {}
					}`),
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult":
							{
								"resource": "mock-resource"
							}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"CanGetQueryFromNestedXRStatusKey": {
			reason: "The Function should be able to get Query from the nested XR status key",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.nestedField.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"nestedField": {
										"azResourceGraphQueryResult":
											{
												"resource": "mock-resource"
											}
									}
								}}`),
						},
					},
				},
			},
		},
		"CanGetQueryFromEnvironmentContextKey": {
			reason: "The Function should be able to get Query from the environment context key",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.[apiextensions.crossplane.io/environment].azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"apiextensions.crossplane.io/environment": {
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								}
							}
						  }`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"CanGetQueryFromNestedContextKey": {
			reason: "The Function should be able to get Query from the nested context key",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "context.nestedField.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"nestedField": {
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								}
							}
						  }`,
					),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"WarningIfQueryIsEmpty": {
			reason: "The Function should return a warning if the query is empty",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "",
						"managementGroups": ["test"],
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_WARNING,
							Message:  "Query is empty",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldSkipQueryWhenStatusTargetHasData": {
			reason: "The Function should skip query when status target already has data",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.azResourceGraphQueryResult",
						"skipQueryWhenTargetHasData": true
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data"
									}
								}
							}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSkip",
							Message: strPtr("Target already has data, skipped query to avoid throttling"),
							Status:  fnv1.Status_STATUS_CONDITION_TRUE,
							Reason:  "SkippedQuery",
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data"
									}
								}
							}`),
						},
					},
				},
			},
		},
		"CanGetQueryFromXRStatusKey": {
			reason: "The Function should be able to get Query from the XR status key",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"managementGroups": ["test"],
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"ShouldUseMultipleSubscriptionsFromInput": {
			reason: "The Function should use multiple subscriptions from input when provided",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptions": ["sub1", "sub2", "sub3"],
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromContext": {
			reason: "The Function should be able to get subscriptions from the context field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "context.subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Context: resource.MustStructJSON(`{
						"subscriptionsList": ["sub1", "sub2", "sub3"]
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"subscriptionsList": ["sub1", "sub2", "sub3"]
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromStatus": {
			reason: "The Function should be able to get subscriptions from the status field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "status.subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"subscriptionsList": ["sub1", "sub2", "sub3"]
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"subscriptionsList": ["sub1", "sub2", "sub3"],
									"azResourceGraphQueryResult":
										{
											"resource": "mock-resource"
										}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromNestedStatus": {
			reason: "The Function should be able to get subscriptions from nested status field using dot notation",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "status.nested.field.subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nested": {
										"field": {
											"subscriptionsList": ["sub1", "sub2", "sub3"]
										}
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"nested": {
										"field": {
											"subscriptionsList": ["sub1", "sub2", "sub3"]
										}
									},
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromBracketStatus": {
			reason: "The Function should be able to get subscriptions from status field using bracket notation",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "status.[complex.field.with.dots].subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"complex.field.with.dots": {
										"subscriptionsList": ["sub1", "sub2", "sub3"]
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"complex.field.with.dots": {
										"subscriptionsList": ["sub1", "sub2", "sub3"]
									},
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromNestedContext": {
			reason: "The Function should be able to get subscriptions from nested context field using dot notation",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "context.nested.field.subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Context: resource.MustStructJSON(`{
						"nested": {
							"field": {
								"subscriptionsList": ["sub1", "sub2", "sub3"]
							}
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"nested": {
							"field": {
								"subscriptionsList": ["sub1", "sub2", "sub3"]
							}
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"CanGetSubscriptionsFromBracketContext": {
			reason: "The Function should be able to get subscriptions from context field using bracket notation",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"subscriptionsRef": "context.[complex.field.with.dots].subscriptionsList",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Context: resource.MustStructJSON(`{
						"complex.field.with.dots": {
							"subscriptionsList": ["sub1", "sub2", "sub3"]
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"complex.field.with.dots": {
							"subscriptionsList": ["sub1", "sub2", "sub3"]
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenNoIntervalSet": {
			reason: "The Function should execute query when queryIntervalMinutes is not set",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenNoLastQueryTime": {
			reason: "The Function should execute query when no lastQueryTime exists even with interval set",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data"
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldSkipQueryWhenWithinInterval": {
			reason: "The Function should skip query when last query was within the specified interval",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data",
										"lastQueryTime": "` + time.Now().Add(-5*time.Minute).Format(time.RFC3339) + `"
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:    "FunctionSkip",
							Message: strPtr("Query skipped due to interval limit (10 minutes)"),
							Status:  fnv1.Status_STATUS_CONDITION_TRUE,
							Reason:  "IntervalLimit",
							Target:  fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data",
										"lastQueryTime": "` + time.Now().Add(-5*time.Minute).Format(time.RFC3339) + `"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenIntervalExpired": {
			reason: "The Function should execute query when the interval has expired",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data",
										"lastQueryTime": "` + time.Now().Add(-15*time.Minute).Format(time.RFC3339) + `"
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenInvalidTimestamp": {
			reason: "The Function should execute query when lastQueryTime is invalid",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data",
										"lastQueryTime": "invalid-timestamp"
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldIgnoreIntervalForContextTargets": {
			reason: "The Function should ignore interval limits for context targets",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "context.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult": {
							"resource": "mock-resource"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWhenIntervalIsZero": {
			reason: "The Function should execute query when queryIntervalMinutes is zero or negative",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 0
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "existing-data",
										"lastQueryTime": "` + time.Now().Add(-1*time.Minute).Format(time.RFC3339) + `"
									}
								}}`),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldWriteLastQueryTimeForStatusTarget": {
			reason: "The Function should write lastQueryTime when queryIntervalMinutes is set for status targets",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource",
										"lastQueryTime": "2024-01-01T12:00:00Z"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldExecuteQueryWithDifferentTargetName": {
			reason: "The Function should write lastQueryTime to any status target name when interval is set",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.vmData",
						"queryIntervalMinutes": 5
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				// This test will use special validation
				rsp: nil,
			},
		},
		"ShouldNotWriteLastQueryTimeForContextTarget": {
			reason: "The Function should not write lastQueryTime for context targets",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "context.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(`{
						"azResourceGraphQueryResult": {
							"resource": "mock-resource"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								}
							}`),
						},
					},
				},
			},
		},
		"ShouldNotWriteLastQueryTimeWhenIntervalNotSet": {
			reason: "The Function should not write lastQueryTime when queryIntervalMinutes is not set",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"metadata": {
									"name": "cool-xr"
								},
								"status": {
									"azResourceGraphQueryResult": {
										"resource": "mock-resource"
									}
								}}`),
						},
					},
				},
			},
		},
		"ShouldAddTimestampToMapResultsOnly": {
			reason: "The Function should only add lastQueryTime to map results (backwards compatibility)",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"query": "Resources| count",
						"target": "status.azResourceGraphQueryResult",
						"queryIntervalMinutes": 10
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: creds},
						},
					},
				},
			},
			want: want{
				// This test verifies backwards compatibility - the mock returns a map
				// so lastQueryTime should be added as a sibling field, not wrapped
				rsp: nil, // Will be validated specially
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Mocking the azQuery function to return a successful result
			mockQuery := &MockAzureQuery{
				AzQueryFunc: func(_ context.Context, _ map[string]string, _ *v1beta1.Input) (armresourcegraph.ClientResourcesResponse, error) {
					return armresourcegraph.ClientResourcesResponse{
						QueryResponse: armresourcegraph.QueryResponse{
							Count:           to.Ptr(int64(1)),
							Data:            map[string]interface{}{"resource": "mock-resource"}, // Mock data
							ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedFalse),
							TotalRecords:    to.Ptr(int64(1)),
							Facets:          nil,
							SkipToken:       nil,
						},
					}, nil
				},
			}
			f := &Function{
				azureQuery: mockQuery,
				log:        logging.NewNopLogger(),
			}
			rsp, err := f.RunFunction(tc.args.ctx, tc.args.req)

			// Special handling for timestamp tests
			if name == "ShouldWriteLastQueryTimeForStatusTarget" || name == "ShouldExecuteQueryWithDifferentTargetName" || name == "ShouldAddTimestampToMapResultsOnly" || name == "ShouldExecuteQueryWhenNoLastQueryTime" || name == "ShouldExecuteQueryWhenInvalidTimestamp" || name == "ShouldExecuteQueryWhenIntervalExpired" {
				// Verify the structure and validate timestamp
				if err := validateLastQueryTimeInResponse(rsp, name); err != nil {
					t.Errorf("%s\n%v", tc.reason, err)
					return
				}
				
				// Additional validation for backwards compatibility test
				if name == "ShouldAddTimestampToMapResultsOnly" {
					if err := validateBackwardsCompatibility(rsp); err != nil {
						t.Errorf("%s\nBackwards compatibility check failed: %v", tc.reason, err)
						return
					}
				}
				// Don't compare exact response for timestamp tests
			} else {
				if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
					t.Errorf("%s\nf.RunFunction(...): -want rsp, +got rsp:\n%s", tc.reason, diff)
				}
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}

func validateLastQueryTimeInResponse(rsp *fnv1.RunFunctionResponse, testName string) error {
	if rsp.Desired == nil || rsp.Desired.Composite == nil {
		return fmt.Errorf("missing desired composite resource")
	}

	// Determine which field to check based on test name
	targetFieldName := "azResourceGraphQueryResult"
	if testName == "ShouldExecuteQueryWithDifferentTargetName" {
		targetFieldName = "vmData"
	}

	// Get status from the resource
	statusValue, exists := rsp.Desired.Composite.Resource.Fields["status"]
	if !exists {
		return fmt.Errorf("missing status field")
	}
	
	status := statusValue.GetStructValue().AsMap()

	// Get the target data directly
	targetData, ok := status[targetFieldName]
	if !ok {
		return fmt.Errorf("missing %s in status", targetFieldName)
	}

	queryResult, ok := targetData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("query result is not a map")
	}

	// Check that resource field exists
	if _, exists := queryResult["resource"]; !exists {
		return fmt.Errorf("missing resource field in query result")
	}

	// Check that lastQueryTime exists and is a valid RFC3339 timestamp
	lastQueryTimeRaw, exists := queryResult["lastQueryTime"]
	if !exists {
		return fmt.Errorf("missing lastQueryTime field")
	}

	lastQueryTimeStr, ok := lastQueryTimeRaw.(string)
	if !ok {
		return fmt.Errorf("lastQueryTime is not a string")
	}

	// Validate timestamp format
	if _, err := time.Parse(time.RFC3339, lastQueryTimeStr); err != nil {
		return fmt.Errorf("invalid lastQueryTime format: %v", err)
	}

	return nil
}

func validateBackwardsCompatibility(rsp *fnv1.RunFunctionResponse) error {
	if rsp.Desired == nil || rsp.Desired.Composite == nil {
		return fmt.Errorf("missing desired composite resource")
	}

	// Get status from the resource
	statusValue, exists := rsp.Desired.Composite.Resource.Fields["status"]
	if !exists {
		return fmt.Errorf("missing status field")
	}
	
	status := statusValue.GetStructValue().AsMap()
	
	// Get the query result
	targetData, ok := status["azResourceGraphQueryResult"]
	if !ok {
		return fmt.Errorf("missing azResourceGraphQueryResult in status")
	}

	queryResult, ok := targetData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("query result is not a map")
	}

	// Verify backwards compatibility - the original "resource" field should be preserved at the top level
	if _, exists := queryResult["resource"]; !exists {
		return fmt.Errorf("original 'resource' field not preserved - backwards compatibility broken")
	}

	// Verify lastQueryTime is added as a sibling, not wrapped
	if _, exists := queryResult["lastQueryTime"]; !exists {
		return fmt.Errorf("lastQueryTime not added as sibling field")
	}

	// Verify there's no "data" wrapper that would break compatibility
	if _, exists := queryResult["data"]; exists {
		return fmt.Errorf("found 'data' wrapper field - this breaks backwards compatibility")
	}

	return nil
}
