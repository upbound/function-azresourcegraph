package main

import (
	"context"
	"testing"

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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
				},
			},
		},
		"ResponseIsReturnedWithOptionalManagementGroups": {
			reason: "The Function should accept optional managmenetGroups input",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
		"ShouldFailWithUnsupportedTarget": {
			reason: "The Function fail in case of unsupported value in Target Field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  `Query: "Resources| count"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "Unrecognized target field: notcool.azResourceGraphQueryResult",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
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
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "context.azResourceGraphQuery",
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
				},
			},
		},
		"CanGetQueryFromNestedContextKey": {
			reason: "The Function should be able to get Query from the nested Context field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "context.somekey.nestedazResourceGraphQuery",
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
					Context: resource.MustStructJSON(
						`{
							"apiextensions.crossplane.io/environment": {
							   "azResourceGraphQuery": "QueryFromEnvironment"
							},
							"somekey" : {
							   "nestedazResourceGraphQuery": "QueryFromNestedKey"
							}
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
							Message:  `Query: "QueryFromNestedKey"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								},
							"apiextensions.crossplane.io/environment": {
							   "azResourceGraphQuery": "QueryFromEnvironment"
							},
							"somekey" : {
							   "nestedazResourceGraphQuery": "QueryFromNestedKey"
							}
						}`,
					),
				},
			},
		},
		"CanGetQueryFromEnvironmentContextKey": {
			reason: "The Function should be able to get Query from the Environment Context field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "context.[apiextensions.crossplane.io/environment].azResourceGraphQuery",
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
					Context: resource.MustStructJSON(
						`{
							"apiextensions.crossplane.io/environment": {
							   "azResourceGraphQuery": "QueryFromEnvironment"
							}
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
							Message:  `Query: "QueryFromEnvironment"`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Context: resource.MustStructJSON(
						`{
							"azResourceGraphQueryResult":
								{
									"resource": "mock-resource"
								},
							"apiextensions.crossplane.io/environment": {
							   "azResourceGraphQuery": "QueryFromEnvironment"
							}
						}`,
					),
				},
			},
		},
		"CanGetQueryFromXRStatusKey": {
			reason: "The Function should be able to get Query from the XR status field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "status.azResourceGraphQuery",
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"azResourceGraphQuery": "QueryFromXRStatus"
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
							Message:  `Query: "QueryFromXRStatus"`,
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
				},
			},
		},
		"CanGetQueryFromNestedXRStatusKey": {
			reason: "The Function should be able to get Query from the nested XR status field",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "status.testKey.azResourceGraphQuery",
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"testKey": {
										"azResourceGraphQuery": "QueryFromNestedXRStatus"
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
							Message:  `Query: "QueryFromNestedXRStatus"`,
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
				},
			},
		},
		"FailIfQueryIsEmpty": {
			reason: "The Function should fail if Query is empty",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "azresourcegraph.fn.crossplane.io/v1alpha1",
						"kind": "Input",
						"queryRef": "status.nonExistingKey.azResourceGraphQuery",
						"target": "context.azResourceGraphQueryResult"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.org/v1",
								"kind": "XR",
								"status": {
									"testKey": {
										"azResourceGraphQuery": "QueryFromNestedXRStatus"
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
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  `Query is empty`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
				},
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

			if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want rsp, +got rsp:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}
