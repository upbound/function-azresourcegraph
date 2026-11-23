package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/upbound/function-azresourcegraph/input/v1beta1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

type MockAzureQuery struct {
	AzQueryFunc func(ctx context.Context, azureCreds interface{}, in *v1beta1.Input, log logging.Logger) (armresourcegraph.ClientResourcesResponse, error)
}

func (m *MockAzureQuery) azQuery(ctx context.Context, azureCreds interface{}, in *v1beta1.Input, log logging.Logger) (armresourcegraph.ClientResourcesResponse, error) {
	return m.AzQueryFunc(ctx, azureCreds, in, log)
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
				AzQueryFunc: func(_ context.Context, _ interface{}, _ *v1beta1.Input, _ logging.Logger) (armresourcegraph.ClientResourcesResponse, error) {
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

func TestIdentityType(t *testing.T) {
	var (
		xr      = `{"apiVersion":"example.org/v1","kind":"XR","metadata":{"name":"cool-xr"}}`
		spInput = `{
	"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
	"kind": "Input",
	"query": "Resources| count",
	"target": "status.azResourceGraphQueryResult"
}`
		wiInput = `{
	"apiVersion": "azresourcegraph.fn.crossplane.io/v1beta1",
	"kind": "Input",
	"query": "Resources| count",
	"target": "status.azResourceGraphQueryResult",
	"identity": {
		"type": "AzureWorkloadIdentityCredentials"
	}
}`
		servicePrincipalCreds = &fnv1.CredentialData{
			Data: map[string][]byte{
				"credentials": []byte(`{
"clientId": "test-client-id",
"clientSecret": "test-client-secret",
"subscriptionId": "test-subscription-id",
"tenantId": "test-tenant-id"
}`),
			},
		}
		workloadIdentityCredentials = &fnv1.CredentialData{
			Data: map[string][]byte{
				"credentials": []byte(`{
"federatedTokenFile": "/var/run/secrets/azure/tokens/azure-identity-token",
"subscriptionId": "test-subscription-id"
}`),
			},
		}
		multipleWorkloadIdentityCredentials = &fnv1.CredentialData{
			Data: map[string][]byte{
				"credentials": []byte(`[
	{
		"federatedTokenFile": "/var/run/secrets/azure/tokens/azure-identity-token",
		"subscriptionId": "sub-id"
	},
	{
		"federatedTokenFile": "/var/run/secrets/azure/tokens/azure-identity-token",
		"subscriptionId": "sub-id2"
	}
]`),
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
		"AzureServicePrincipalCredentialsImplicit": {
			reason: "The Function should default to identity.type AzureServicePrincipalCredentials",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta:  &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(spInput),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: servicePrincipalCreds},
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
							Message:  `failed to initialize service principal provider: failed to obtain clientsecret credentials`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
		},
		"AzureServicePrincipalCredentialsExplicit": {
			reason: "The Function should use ServicePrincipal credentials if identity.type is AzureServicePrincipalCredentials",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta:  &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(spInput),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: servicePrincipalCreds},
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
							Message:  `failed to initialize service principal provider: failed to obtain clientsecret credentials`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
		},
		"AzureWorkloadIdentityCredentialsMultipleCredentials": {
			reason: "The Function should failt if identity.type is AzureWorkloadIdentityCredentials and credentials array has multiple entries",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta:  &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(wiInput),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: multipleWorkloadIdentityCredentials},
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
							Message:  `invalid credential format: workload identity support only one credentials entry`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
		},
		"AzureWorkloadIdentityCredentialsValid": {
			reason: "The Function should use Workload Identity credentials if identity.type is AzureWorkloadIdentityCredentials",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Meta:  &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(wiInput),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
					Credentials: map[string]*fnv1.Credentials{
						"azure-creds": {
							Source: &fnv1.Credentials_CredentialData{CredentialData: workloadIdentityCredentials},
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
							Message:  `failed to initialize workload identity provider: failed to obtain workloadidentity credentials`,
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(xr),
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create mock responders for each type of query
			mockQuery := &MockAzureQuery{
				AzQueryFunc: func(_ context.Context, credentials interface{}, in *v1beta1.Input, _ logging.Logger) (armresourcegraph.ClientResourcesResponse, error) {
					identityType := v1beta1.IdentityTypeAzureServicePrincipalCredentials

					if in.Identity != nil && in.Identity.Type != "" {
						identityType = in.Identity.Type
					}

					switch credentials.(type) {
					case map[string]string:
					case []map[string]string:
						if identityType == v1beta1.IdentityTypeAzureWorkloadIdentityCredentials {
							return armresourcegraph.ClientResourcesResponse{}, errors.New("invalid credential format: workload identity support only one credentials entry")
						}
					default:
						return armresourcegraph.ClientResourcesResponse{}, errors.New("invalid credential format")
					}

					switch identityType {
					case v1beta1.IdentityTypeAzureWorkloadIdentityCredentials:
						return armresourcegraph.ClientResourcesResponse{}, errors.New("failed to initialize workload identity provider: failed to obtain workloadidentity credentials")
					case v1beta1.IdentityTypeAzureServicePrincipalCredentials:
						return armresourcegraph.ClientResourcesResponse{}, errors.New("failed to initialize service principal provider: failed to obtain clientsecret credentials")
					default:
						return armresourcegraph.ClientResourcesResponse{}, errors.Errorf("unsupported identity.type: %s", string(identityType))
					}
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

func validateLastQueryTimeInResponse(rsp *fnv1.RunFunctionResponse, testName string) error {
	if rsp.GetDesired() == nil || rsp.GetDesired().GetComposite() == nil {
		return fmt.Errorf("missing desired composite resource")
	}

	// Determine which field to check based on test name
	targetFieldName := "azResourceGraphQueryResult"
	if testName == "ShouldExecuteQueryWithDifferentTargetName" {
		targetFieldName = "vmData"
	}

	// Get status from the resource
	statusValue, exists := rsp.GetDesired().GetComposite().GetResource().GetFields()["status"]
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
		return fmt.Errorf("invalid lastQueryTime format: %w", err)
	}

	return nil
}

func validateBackwardsCompatibility(rsp *fnv1.RunFunctionResponse) error {
	if rsp.GetDesired() == nil || rsp.GetDesired().GetComposite() == nil {
		return fmt.Errorf("missing desired composite resource")
	}

	// Get status from the resource
	statusValue, exists := rsp.GetDesired().GetComposite().GetResource().GetFields()["status"]
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

// callMock records calls and inputs of AzQuery and returns a scripted response.
type callMock struct {
	calls  int
	inputs []*v1beta1.Input
	resp   armresourcegraph.ClientResourcesResponse
	err    error
}

func (m *callMock) azQuery(_ context.Context, _ interface{}, in *v1beta1.Input, _ logging.Logger) (armresourcegraph.ClientResourcesResponse, error) {
	m.calls++
	m.inputs = append(m.inputs, in)
	return m.resp, m.err
}

const (
	fixXR        = `{"apiVersion":"example.org/v1","kind":"XR","metadata":{"name":"cool-xr"}}`
	fixSPCreds   = `{"clientId":"id","clientSecret":"secret","subscriptionId":"sub","tenantId":"tenant"}`
	fixInputHead = `"apiVersion":"azresourcegraph.fn.crossplane.io/v1beta1","kind":"Input","query":"Resources| count","target":"status.result"`
)

func fixCredData(key, value string) map[string]*fnv1.Credentials {
	return map[string]*fnv1.Credentials{
		"azure-creds": {
			Source: &fnv1.Credentials_CredentialData{CredentialData: &fnv1.CredentialData{
				Data: map[string][]byte{key: []byte(value)},
			}},
		},
	}
}

func fixXRWithStatus(t *testing.T, status map[string]interface{}) *fnv1.Resource {
	t.Helper()
	xr := map[string]interface{}{}
	if err := json.Unmarshal([]byte(fixXR), &xr); err != nil {
		t.Fatal(err)
	}
	if status != nil {
		xr["status"] = status
	}
	b, err := json.Marshal(xr)
	if err != nil {
		t.Fatal(err)
	}
	return &fnv1.Resource{Resource: resource.MustStructJSON(string(b))}
}

func fixHasCondition(rsp *fnv1.RunFunctionResponse, typ, reason string) bool {
	for _, c := range rsp.GetConditions() {
		if c.GetType() == typ && c.GetReason() == reason {
			return true
		}
	}
	return false
}

func fixDesiredStatus(rsp *fnv1.RunFunctionResponse) map[string]interface{} {
	return rsp.GetDesired().GetComposite().GetResource().GetFields()["status"].GetStructValue().AsMap()
}

func fixResultsBySeverity(rsp *fnv1.RunFunctionResponse, sev fnv1.Severity) []string {
	var out []string
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == sev {
			out = append(out, r.GetMessage())
		}
	}
	return out
}

func TestRunFunctionFixes(t *testing.T) {
	now := time.Now()
	ts := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	arrayResp := armresourcegraph.ClientResourcesResponse{
		QueryResponse: armresourcegraph.QueryResponse{
			Data: []interface{}{map[string]interface{}{"name": "vm1"}},
		},
	}
	truncated := arrayResp
	truncated.ResultTruncated = to.Ptr(armresourcegraph.ResultTruncatedTrue)
	rowsResp := func(n int, token *string) armresourcegraph.ClientResourcesResponse {
		rows := make([]interface{}, n)
		for i := range rows {
			rows[i] = map[string]interface{}{"name": fmt.Sprintf("vm%d", i)}
		}
		return armresourcegraph.ClientResourcesResponse{QueryResponse: armresourcegraph.QueryResponse{Data: rows, SkipToken: token}}
	}
	capWarn := func(limit, written int) string {
		return fmt.Sprintf("Azure Resource Graph has more rows than maxResults (%d); only the first %d were written, raise maxResults to write more", limit, written)
	}

	type tc struct {
		reason    string
		input     string
		creds     map[string]*fnv1.Credentials
		observed  map[string]interface{}
		desired   map[string]interface{}
		context   map[string]interface{}
		resp      armresourcegraph.ClientResourcesResponse
		wantCalls int
		check     func(t *testing.T, rsp *fnv1.RunFunctionResponse, m *callMock)
	}
	spCreds := fixCredData("credentials", fixSPCreds)
	unresolvedWarn := func(ref string) string {
		return fmt.Sprintf("subscriptionsRef %q did not resolve to a non-empty list of subscription IDs, skipping query", ref)
	}
	unresolved := func(ref string, status map[string]interface{}) tc {
		return tc{
			reason:   "An unresolved subscriptionsRef should warn and skip the query",
			input:    `{` + fixInputHead + `,"subscriptionsRef":"` + ref + `"}`,
			creds:    spCreds,
			observed: status,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{unresolvedWarn(ref)}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
			},
		}
	}

	cases := map[string]tc{
		"UnknownIdentityType": {
			reason: "An unknown identity.type should be rejected before querying",
			input:  `{` + fixInputHead + `,"identity":{"type":"AzureWorkloadIdentity"}}`,
			creds:  spCreds,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				want := []string{`unsupported identity.type "AzureWorkloadIdentity": must be AzureServicePrincipalCredentials or AzureWorkloadIdentityCredentials`}
				if diff := cmp.Diff(want, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_FATAL)); diff != "" {
					t.Errorf("fatals: -want +got:\n%s", diff)
				}
				if len(rsp.GetResults()) != 1 {
					t.Errorf("want exactly one result, got %d", len(rsp.GetResults()))
				}
			},
		},
		"SecretWithoutCredentialsKey": {
			reason: "A secret without the credentials key should be rejected",
			input:  `{` + fixInputHead + `}`,
			creds:  fixCredData("creds", fixSPCreds),
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				want := []string{`azure-creds credential has no "credentials" key`}
				if diff := cmp.Diff(want, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_FATAL)); diff != "" {
					t.Errorf("fatals: -want +got:\n%s", diff)
				}
			},
		},
		"IntervalBehindStatusWritingStep": {
			reason:   "Interval should apply when an earlier step wrote other status fields",
			input:    `{` + fixInputHead + `,"queryIntervalMinutes":60}`,
			creds:    spCreds,
			observed: map[string]interface{}{"result": []interface{}{map[string]interface{}{"name": "x"}, map[string]interface{}{"lastQueryTime": ts(time.Minute)}}},
			desired:  map[string]interface{}{"marker": "m"},
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if !fixHasCondition(rsp, "FunctionSkip", "IntervalLimit") {
					t.Errorf("want FunctionSkip/IntervalLimit, got %v", rsp.GetConditions())
				}
				st := fixDesiredStatus(rsp)
				if st["marker"] != "m" {
					t.Errorf("desired status lost marker: %v", st)
				}
				if got, ok := st["result"].([]interface{}); !ok || len(got) != 2 {
					t.Errorf("desired status lost target: %v", st)
				}
			},
		},
		"SkipWhenTargetHasDataBehindStatusWritingStep": {
			reason:   "skipQueryWhenTargetHasData should apply when an earlier step wrote other status fields",
			input:    `{` + fixInputHead + `,"skipQueryWhenTargetHasData":true}`,
			creds:    spCreds,
			observed: map[string]interface{}{"result": []interface{}{map[string]interface{}{"name": "x"}}},
			desired:  map[string]interface{}{"marker": "m"},
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if !fixHasCondition(rsp, "FunctionSkip", "SkippedQuery") {
					t.Errorf("want FunctionSkip/SkippedQuery, got %v", rsp.GetConditions())
				}
			},
		},
		"EmptyFirstResultRequeriesAfterInterval": {
			reason:    "A lastQueryTime-only target has no data, so an elapsed interval should re-query",
			input:     `{` + fixInputHead + `,"queryIntervalMinutes":1,"skipQueryWhenTargetHasData":true}`,
			creds:     spCreds,
			observed:  map[string]interface{}{"result": []interface{}{map[string]interface{}{"lastQueryTime": ts(5 * time.Minute)}}},
			resp:      arrayResp,
			wantCalls: 1,
		},
		"ArrayResultNoPreviousData": {
			reason:    "Array results should get a trailing lastQueryTime element",
			input:     `{` + fixInputHead + `,"queryIntervalMinutes":10}`,
			creds:     spCreds,
			resp:      arrayResp,
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				got, ok := fixDesiredStatus(rsp)["result"].([]interface{})
				if !ok || len(got) != 2 {
					t.Fatalf("want 2 elements, got %v", fixDesiredStatus(rsp))
				}
				last, _ := got[1].(map[string]interface{})
				parsed, err := time.Parse(time.RFC3339, fmt.Sprint(last["lastQueryTime"]))
				if len(last) != 1 || err != nil || time.Since(parsed) > time.Minute {
					t.Errorf("bad lastQueryTime element: %v", got[1])
				}
			},
		},
		"ArrayResultIntervalElapsed": {
			reason:    "An elapsed interval on array results should re-query",
			input:     `{` + fixInputHead + `,"queryIntervalMinutes":10}`,
			creds:     spCreds,
			observed:  map[string]interface{}{"result": []interface{}{map[string]interface{}{"name": "vm1"}, map[string]interface{}{"lastQueryTime": ts(15 * time.Minute)}}},
			resp:      arrayResp,
			wantCalls: 1,
		},
		"ArrayResultIntervalNotElapsed": {
			reason:   "A running interval on array results should skip",
			input:    `{` + fixInputHead + `,"queryIntervalMinutes":10}`,
			creds:    spCreds,
			observed: map[string]interface{}{"result": []interface{}{map[string]interface{}{"name": "vm1"}, map[string]interface{}{"lastQueryTime": ts(5 * time.Minute)}}},
			resp:     arrayResp,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if !fixHasCondition(rsp, "FunctionSkip", "IntervalLimit") {
					t.Errorf("want FunctionSkip/IntervalLimit, got %v", rsp.GetConditions())
				}
			},
		},
		"SubscriptionsRefStatus": {
			reason:    "subscriptionsRef from status should resolve",
			input:     `{` + fixInputHead + `,"subscriptionsRef":"status.subs"}`,
			creds:     spCreds,
			observed:  map[string]interface{}{"subs": []interface{}{"s1", "s2"}},
			resp:      arrayResp,
			wantCalls: 1,
			check: func(t *testing.T, _ *fnv1.RunFunctionResponse, m *callMock) {
				if len(m.inputs) < 1 {
					t.Fatalf("want a captured input, got %d", len(m.inputs))
				}
				if diff := cmp.Diff([]*string{to.Ptr("s1"), to.Ptr("s2")}, m.inputs[0].Subscriptions); diff != "" {
					t.Errorf("subscriptions: -want +got:\n%s", diff)
				}
			},
		},
		"SubscriptionsRefContextBracketPath": {
			reason:    "subscriptionsRef from context with a bracket path should resolve",
			input:     `{` + fixInputHead + `,"subscriptionsRef":"context.[apiextensions.crossplane.io/environment].subscriptions"}`,
			creds:     spCreds,
			context:   map[string]interface{}{"apiextensions.crossplane.io/environment": map[string]interface{}{"subscriptions": []interface{}{"s1", "s2"}}},
			resp:      arrayResp,
			wantCalls: 1,
			check: func(t *testing.T, _ *fnv1.RunFunctionResponse, m *callMock) {
				if len(m.inputs) < 1 {
					t.Fatalf("want a captured input, got %d", len(m.inputs))
				}
				if diff := cmp.Diff([]*string{to.Ptr("s1"), to.Ptr("s2")}, m.inputs[0].Subscriptions); diff != "" {
					t.Errorf("subscriptions: -want +got:\n%s", diff)
				}
			},
		},
		"SubscriptionsRefStatusMissing":         unresolved("status.subs", nil),
		"SubscriptionsRefStatusNotList":         unresolved("status.subs", map[string]interface{}{"subs": "s1"}),
		"SubscriptionsRefStatusEmptyList":       unresolved("status.subs", map[string]interface{}{"subs": []interface{}{}}),
		"SubscriptionsRefStatusNonStringItems":  unresolved("status.subs", map[string]interface{}{"subs": []interface{}{"s1", 2}}),
		"SubscriptionsRefStatusEmptyStringItem": unresolved("status.subs", map[string]interface{}{"subs": []interface{}{"s1", ""}}),
		"SubscriptionsRefContextMissing":        unresolved("context.subs", nil),
		"TruncatedResultWarns": {
			reason:    "Truncated results should produce a warning",
			input:     `{` + fixInputHead + `}`,
			creds:     spCreds,
			resp:      truncated,
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				want := []string{"Azure Resource Graph returned truncated results and not all rows were written; paging is not possible when the query uses limit, take or sample, or when all output columns are dynamic or null"}
				if diff := cmp.Diff(want, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
				if !fixHasCondition(rsp, "FunctionSuccess", "Success") {
					t.Errorf("want FunctionSuccess, got %v", rsp.GetConditions())
				}
				if diff := cmp.Diff([]interface{}{map[string]interface{}{"name": "vm1"}}, fixDesiredStatus(rsp)["result"]); diff != "" {
					t.Errorf("status target: -want +got:\n%s", diff)
				}
			},
		},
		"QueryRefDesiredOverObserved": {
			reason:    "queryRef should resolve to the desired XR status over the observed one",
			input:     `{` + fixInputHead + `,"queryRef":"status.q"}`,
			creds:     spCreds,
			observed:  map[string]interface{}{"q": "Resources | old"},
			desired:   map[string]interface{}{"q": "Resources | new"},
			resp:      arrayResp,
			wantCalls: 1,
			check: func(t *testing.T, _ *fnv1.RunFunctionResponse, m *callMock) {
				if len(m.inputs) < 1 {
					t.Fatalf("want a captured input, got %d", len(m.inputs))
				}
				if m.inputs[0].Query != "Resources | new" {
					t.Errorf("want query %q, got %q", "Resources | new", m.inputs[0].Query)
				}
			},
		},
		"MaxResultsTrimsRows": {
			reason:    "Rows above maxResults should be trimmed with a warning",
			input:     `{` + fixInputHead + `,"maxResults":3}`,
			creds:     spCreds,
			resp:      rowsResp(5, nil),
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{capWarn(3, 3)}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
				want := []interface{}{map[string]interface{}{"name": "vm0"}, map[string]interface{}{"name": "vm1"}, map[string]interface{}{"name": "vm2"}}
				if diff := cmp.Diff(want, fixDesiredStatus(rsp)["result"]); diff != "" {
					t.Errorf("status target: -want +got:\n%s", diff)
				}
			},
		},
		"MoreRowsAvailableWarns": {
			reason:    "A remaining SkipToken should warn when rows reach maxResults",
			input:     `{` + fixInputHead + `,"maxResults":2}`,
			creds:     spCreds,
			resp:      rowsResp(2, to.Ptr("x")),
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{capWarn(2, 2)}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
				got, ok := fixDesiredStatus(rsp)["result"].([]interface{})
				if !ok || len(got) != 2 {
					t.Errorf("want 2 rows, got %v", fixDesiredStatus(rsp)["result"])
				}
			},
		},
		"DefaultMaxResultsWarns": {
			reason:    "Without maxResults the default of 1000 rows should apply",
			input:     `{` + fixInputHead + `}`,
			creds:     spCreds,
			resp:      rowsResp(1000, to.Ptr("x")),
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{capWarn(1000, 1000)}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
				got, ok := fixDesiredStatus(rsp)["result"].([]interface{})
				if !ok || len(got) != 1000 {
					t.Errorf("want 1000 rows, got %d", len(got))
				}
			},
		},
		"NormalSuccessNoWarning": {
			reason:    "A complete result within maxResults should not warn",
			input:     `{` + fixInputHead + `}`,
			creds:     spCreds,
			resp:      rowsResp(2, nil),
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if w := fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING); len(w) != 0 {
					t.Errorf("want no warnings, got %v", w)
				}
			},
		},
		"MaxResultsExactRowsNoWarning": {
			reason:    "Exactly maxResults rows without a SkipToken should not warn",
			input:     `{` + fixInputHead + `,"maxResults":2}`,
			creds:     spCreds,
			resp:      rowsResp(2, nil),
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if w := fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING); len(w) != 0 {
					t.Errorf("want no warnings, got %v", w)
				}
				want := []interface{}{map[string]interface{}{"name": "vm0"}, map[string]interface{}{"name": "vm1"}}
				if diff := cmp.Diff(want, fixDesiredStatus(rsp)["result"]); diff != "" {
					t.Errorf("status target: -want +got:\n%s", diff)
				}
			},
		},
		"MaxResultsNegativeRejected": {
			reason: "A negative maxResults should be rejected before querying",
			input:  `{` + fixInputHead + `,"maxResults":-1}`,
			creds:  spCreds,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				want := []string{"maxResults must be at least 1, got -1"}
				if diff := cmp.Diff(want, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_FATAL)); diff != "" {
					t.Errorf("fatals: -want +got:\n%s", diff)
				}
			},
		},
		"MaxResultsZeroRejected": {
			reason: "maxResults below 1 should be rejected before querying",
			input:  `{` + fixInputHead + `,"maxResults":0}`,
			creds:  spCreds,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				want := []string{"maxResults must be at least 1, got 0"}
				if diff := cmp.Diff(want, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_FATAL)); diff != "" {
					t.Errorf("fatals: -want +got:\n%s", diff)
				}
			},
		},
		"SubscriptionsRefUnresolvedWithStaticSubscriptions": {
			reason:   "An unresolved subscriptionsRef should skip even when static subscriptions are set",
			input:    `{` + fixInputHead + `,"subscriptions":["s"],"subscriptionsRef":"status.subs"}`,
			creds:    spCreds,
			observed: nil,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{unresolvedWarn("status.subs")}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
			},
		},
		"SubscriptionsRefContextNotList": {
			reason:  "A context ref resolving to a non-list should warn and skip the query",
			input:   `{` + fixInputHead + `,"subscriptionsRef":"context.subs"}`,
			creds:   spCreds,
			context: map[string]interface{}{"subs": "s1"},
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{unresolvedWarn("context.subs")}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
			},
		},
		"SubscriptionsRefContextEmptyList": {
			reason:  "A context ref resolving to an empty list should warn and skip the query",
			input:   `{` + fixInputHead + `,"subscriptionsRef":"context.subs"}`,
			creds:   spCreds,
			context: map[string]interface{}{"subs": []interface{}{}},
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if diff := cmp.Diff([]string{unresolvedWarn("context.subs")}, fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_WARNING)); diff != "" {
					t.Errorf("warnings: -want +got:\n%s", diff)
				}
			},
		},
		"EmptyIdentityTypeAccepted": {
			reason:    "An empty identity.type should use the default and not be fatal",
			input:     `{` + fixInputHead + `,"identity":{"type":""}}`,
			creds:     spCreds,
			resp:      arrayResp,
			wantCalls: 1,
			check: func(t *testing.T, rsp *fnv1.RunFunctionResponse, _ *callMock) {
				if f := fixResultsBySeverity(rsp, fnv1.Severity_SEVERITY_FATAL); len(f) != 0 {
					t.Errorf("want no fatals, got %v", f)
				}
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			req := &fnv1.RunFunctionRequest{
				Meta:        &fnv1.RequestMeta{Tag: "hello"},
				Input:       resource.MustStructJSON(c.input),
				Observed:    &fnv1.State{Composite: fixXRWithStatus(t, c.observed)},
				Credentials: c.creds,
			}
			if c.desired != nil {
				req.Desired = &fnv1.State{Composite: fixXRWithStatus(t, c.desired)}
			}
			if c.context != nil {
				ctxStruct, err := structpb.NewStruct(c.context)
				if err != nil {
					t.Fatal(err)
				}
				req.Context = ctxStruct
			}
			m := &callMock{resp: c.resp}
			f := &Function{azureQuery: m, log: logging.NewNopLogger()}
			rsp, err := f.RunFunction(context.Background(), req)
			if err != nil {
				t.Fatalf("%s\nRunFunction returned error: %v", c.reason, err)
			}
			if m.calls != c.wantCalls {
				t.Errorf("%s\nwant %d azQuery calls, got %d", c.reason, c.wantCalls, m.calls)
			}
			if c.check != nil {
				c.check(t, rsp, m)
			}
		})
	}
}

func TestMergeStatus(t *testing.T) {
	cases := map[string]struct {
		observed map[string]interface{}
		desired  map[string]interface{}
		want     map[string]interface{}
	}{
		"NestedMapsMerged": {
			observed: map[string]interface{}{"a": map[string]interface{}{"x": 1, "y": 2}},
			desired:  map[string]interface{}{"a": map[string]interface{}{"y": 3, "z": 4}},
			want:     map[string]interface{}{"a": map[string]interface{}{"x": 1, "y": 3, "z": 4}},
		},
		"DesiredWinsOnScalar": {
			observed: map[string]interface{}{"a": "old", "b": 1},
			desired:  map[string]interface{}{"a": "new"},
			want:     map[string]interface{}{"a": "new", "b": 1},
		},
		"ListsReplaced": {
			observed: map[string]interface{}{"a": []interface{}{1, 2, 3}},
			desired:  map[string]interface{}{"a": []interface{}{4}},
			want:     map[string]interface{}{"a": []interface{}{4}},
		},
		"DesiredMapReplacesScalar": {
			observed: map[string]interface{}{"a": "s"},
			desired:  map[string]interface{}{"a": map[string]interface{}{"x": 1}},
			want:     map[string]interface{}{"a": map[string]interface{}{"x": 1}},
		},
		"DesiredScalarReplacesMap": {
			observed: map[string]interface{}{"a": map[string]interface{}{"x": 1}},
			desired:  map[string]interface{}{"a": "s"},
			want:     map[string]interface{}{"a": "s"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			obsBefore, _ := json.Marshal(tc.observed)
			desBefore, _ := json.Marshal(tc.desired)
			got := mergeStatus(tc.observed, tc.desired)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mergeStatus(...): -want +got:\n%s", diff)
			}
			obsAfter, _ := json.Marshal(tc.observed)
			desAfter, _ := json.Marshal(tc.desired)
			if !bytes.Equal(obsBefore, obsAfter) || !bytes.Equal(desBefore, desAfter) {
				t.Errorf("mergeStatus mutated its inputs")
			}
		})
	}
}

func TestTargetHasData(t *testing.T) {
	lq := map[string]interface{}{"lastQueryTime": "2024-01-01T12:00:00Z"}
	cases := map[string]struct {
		data map[string]interface{}
		want bool
	}{
		"ArrayLastQueryTimeOnly": {data: map[string]interface{}{"k": []interface{}{lq}}, want: false},
		"ArrayDataAndLastQueryTime": {data: map[string]interface{}{"k": []interface{}{
			map[string]interface{}{"name": "x"}, lq,
		}}, want: true},
		"MapLastQueryTimeOnly":    {data: map[string]interface{}{"k": lq}, want: false},
		"MapDataAndLastQueryTime": {data: map[string]interface{}{"k": map[string]interface{}{"a": 1, "lastQueryTime": "t"}}, want: true},
		"EmptyArray":              {data: map[string]interface{}{"k": []interface{}{}}, want: false},
		"MissingKey":              {data: map[string]interface{}{}, want: false},
		"NonEmptyString":          {data: map[string]interface{}{"k": "v"}, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := targetHasData(tc.data, "k")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("targetHasData() = %v, want %v", got, tc.want)
			}
		})
	}
}

// fakeResourcesClient records requests and returns scripted pages.
type fakeResourcesClient struct {
	pages []armresourcegraph.ClientResourcesResponse
	err   error
	// errOnCall fails only that 1-based call; 0 means never.
	errOnCall int
	reqs      []armresourcegraph.QueryRequest
}

func (c *fakeResourcesClient) Resources(_ context.Context, q armresourcegraph.QueryRequest, _ *armresourcegraph.ClientResourcesOptions) (armresourcegraph.ClientResourcesResponse, error) {
	i := len(c.reqs)
	opts := q.Options
	if opts != nil {
		cp := *opts
		q.Options = &cp
	}
	c.reqs = append(c.reqs, q)
	if len(c.reqs) > len(c.pages)+2 {
		return armresourcegraph.ClientResourcesResponse{}, errors.New("fake: too many calls")
	}
	if c.err != nil {
		return armresourcegraph.ClientResourcesResponse{}, c.err
	}
	if c.errOnCall == len(c.reqs) {
		return armresourcegraph.ClientResourcesResponse{}, errors.New("boom")
	}
	if i >= len(c.pages) {
		i = len(c.pages) - 1
	}
	return c.pages[i], nil
}

func page(token *string, trunc *armresourcegraph.ResultTruncated, rows ...interface{}) armresourcegraph.ClientResourcesResponse {
	return armresourcegraph.ClientResourcesResponse{QueryResponse: armresourcegraph.QueryResponse{
		Data:            rows,
		Count:           to.Ptr(int64(len(rows))),
		SkipToken:       token,
		ResultTruncated: trunc,
	}}
}

func TestAzQuery(t *testing.T) {
	spCreds := map[string]string{TenantID: "tenant", ClientID: "id", ClientSecret: "secret", SubscriptionID: "cred-sub"}
	baseIn := func() *v1beta1.Input { return &v1beta1.Input{Query: "Resources", Target: "status.r"} }
	row := func(n string) interface{} { return map[string]interface{}{"name": n} }

	newAQ := func(c *fakeResourcesClient) *AzureQuery {
		return &AzureQuery{newClient: func(azcore.TokenCredential) (resourcesClient, error) { return c, nil }}
	}

	t.Run("UnsupportedIdentity", func(t *testing.T) {
		c := &fakeResourcesClient{}
		in := baseIn()
		in.Identity = &v1beta1.Identity{Type: "Bogus"}
		_, err := newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger())
		if err == nil || !strings.Contains(err.Error(), "unsupported identity.type") {
			t.Errorf("want unsupported identity.type error, got %v", err)
		}
		if len(c.reqs) != 0 {
			t.Errorf("client must not be called, got %d calls", len(c.reqs))
		}
	})

	t.Run("WorkloadIdentityMultipleEntries", func(t *testing.T) {
		c := &fakeResourcesClient{}
		in := baseIn()
		in.Identity = &v1beta1.Identity{Type: v1beta1.IdentityTypeAzureWorkloadIdentityCredentials}
		_, err := newAQ(c).azQuery(context.Background(), []map[string]string{{}, {}}, in, logging.NewNopLogger())
		if err == nil || err.Error() != "invalid credential format: workload identity support only one credentials entry" {
			t.Errorf("unexpected error: %v", err)
		}
		if len(c.reqs) != 0 {
			t.Errorf("client must not be called, got %d calls", len(c.reqs))
		}
	})

	t.Run("SingleServicePrincipalSubscription", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(nil, nil, row("a"))}}
		_, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 1 {
			t.Fatalf("want 1 call, got %d", len(c.reqs))
		}
		if diff := cmp.Diff([]*string{to.Ptr("cred-sub")}, c.reqs[0].Subscriptions); diff != "" {
			t.Errorf("subscriptions: -want +got:\n%s", diff)
		}
	})

	t.Run("InputSubscriptionsOverrideCredentials", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(nil, nil, row("a"))}}
		in := baseIn()
		in.Subscriptions = []*string{to.Ptr("in-sub")}
		if _, err := newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger()); err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) < 1 {
			t.Fatalf("want at least 1 call, got %d", len(c.reqs))
		}
		if diff := cmp.Diff([]*string{to.Ptr("in-sub")}, c.reqs[0].Subscriptions); diff != "" {
			t.Errorf("subscriptions: -want +got:\n%s", diff)
		}
	})

	t.Run("TwoPages", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{
			page(to.Ptr("t1"), nil, row("a"), row("b")),
			page(nil, nil, row("c")),
		}}
		in := baseIn()
		in.ManagementGroups = []*string{to.Ptr("mg1")}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if got.ResultTruncated == nil || *got.ResultTruncated != armresourcegraph.ResultTruncatedFalse {
			t.Errorf("want ResultTruncatedFalse, got %v", got.ResultTruncated)
		}
		if diff := cmp.Diff([]interface{}{row("a"), row("b"), row("c")}, got.Data); diff != "" {
			t.Errorf("data: -want +got:\n%s", diff)
		}
		if got.Count == nil || *got.Count != 3 {
			t.Errorf("want count 3, got %v", got.Count)
		}
		if got.SkipToken != nil {
			t.Errorf("want nil SkipToken, got %v", *got.SkipToken)
		}
		if len(c.reqs) != 2 {
			t.Fatalf("want 2 calls, got %d", len(c.reqs))
		}
		if c.reqs[1].Options == nil || c.reqs[1].Options.SkipToken == nil || *c.reqs[1].Options.SkipToken != "t1" {
			t.Errorf("2nd call must carry SkipToken t1, got %+v", c.reqs[1].Options)
		}
		if c.reqs[0].Query == nil || *c.reqs[0].Query == "" || c.reqs[1].Query == nil || *c.reqs[0].Query != *c.reqs[1].Query {
			t.Errorf("2nd call must repeat the query, got %v and %v", c.reqs[0].Query, c.reqs[1].Query)
		}
		if len(c.reqs[0].Subscriptions) == 0 || cmp.Diff(c.reqs[0].Subscriptions, c.reqs[1].Subscriptions) != "" {
			t.Errorf("2nd call must repeat the subscriptions, got %v and %v", c.reqs[0].Subscriptions, c.reqs[1].Subscriptions)
		}
		if len(c.reqs[0].ManagementGroups) == 0 || cmp.Diff(c.reqs[0].ManagementGroups, c.reqs[1].ManagementGroups) != "" {
			t.Errorf("2nd call must repeat the management groups, got %v and %v", c.reqs[0].ManagementGroups, c.reqs[1].ManagementGroups)
		}
	})

	t.Run("MaxRowsStopsPaging", func(t *testing.T) {
		pages := func() []armresourcegraph.ClientResourcesResponse {
			return []armresourcegraph.ClientResourcesResponse{
				page(to.Ptr("t1"), nil, row("a"), row("b")),
				page(to.Ptr("t2"), nil, row("c"), row("d")),
				page(nil, nil, row("e"), row("f")),
			}
		}
		c := &fakeResourcesClient{pages: pages()}
		in := baseIn()
		in.MaxResults = to.Ptr(3)
		got, err := newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 2 {
			t.Errorf("want 2 calls, got %d", len(c.reqs))
		}
		if got.SkipToken == nil || *got.SkipToken != "t2" {
			t.Errorf("want SkipToken t2, got %v", got.SkipToken)
		}

		c = &fakeResourcesClient{pages: pages()}
		in.MaxResults = to.Ptr(100)
		got, err = newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 3 {
			t.Fatalf("want 3 calls, got %d", len(c.reqs))
		}
		for i, tok := range map[int]string{1: "t1", 2: "t2"} {
			if c.reqs[i].Options == nil || c.reqs[i].Options.SkipToken == nil || *c.reqs[i].Options.SkipToken != tok {
				t.Errorf("call %d must carry SkipToken %s, got %+v", i+1, tok, c.reqs[i].Options)
			}
		}
		if rows, _ := got.Data.([]interface{}); len(rows) != 6 {
			t.Errorf("want 6 rows, got %v", got.Data)
		}
		if got.SkipToken != nil {
			t.Errorf("want nil SkipToken, got %v", *got.SkipToken)
		}
	})

	t.Run("MaxRowsEqualsFirstPage", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{
			page(to.Ptr("t1"), nil, row("a"), row("b")),
			page(nil, nil, row("c")),
		}}
		in := baseIn()
		in.MaxResults = to.Ptr(2)
		got, err := newAQ(c).azQuery(context.Background(), spCreds, in, logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 1 {
			t.Errorf("want 1 call, got %d", len(c.reqs))
		}
		if diff := cmp.Diff([]interface{}{row("a"), row("b")}, got.Data); diff != "" {
			t.Errorf("data: -want +got:\n%s", diff)
		}
		if got.SkipToken == nil || *got.SkipToken != "t1" {
			t.Errorf("want SkipToken t1, got %v", got.SkipToken)
		}
	})

	t.Run("RepeatedSkipToken", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{
			page(to.Ptr("t1"), nil, row("a")),
			page(to.Ptr("t1"), nil, row("b")),
		}}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 2 {
			t.Errorf("want 2 calls, got %d", len(c.reqs))
		}
		if got.ResultTruncated == nil || *got.ResultTruncated != armresourcegraph.ResultTruncatedTrue {
			t.Errorf("want ResultTruncatedTrue, got %v", got.ResultTruncated)
		}
	})

	t.Run("NonArrayLaterPage", func(t *testing.T) {
		later := armresourcegraph.ClientResourcesResponse{QueryResponse: armresourcegraph.QueryResponse{Data: map[string]interface{}{"x": 1}}}
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(to.Ptr("t1"), nil, row("a")), later}}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 2 {
			t.Errorf("want 2 calls, got %d", len(c.reqs))
		}
		if diff := cmp.Diff([]interface{}{row("a")}, got.Data); diff != "" {
			t.Errorf("data: -want +got:\n%s", diff)
		}
		if got.ResultTruncated == nil || *got.ResultTruncated != armresourcegraph.ResultTruncatedTrue {
			t.Errorf("want ResultTruncatedTrue, got %v", got.ResultTruncated)
		}
	})

	t.Run("TruncatedFirstPageNoToken", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(nil, to.Ptr(armresourcegraph.ResultTruncatedTrue), row("a"))}}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 1 {
			t.Errorf("want 1 call, got %d", len(c.reqs))
		}
		if diff := cmp.Diff([]interface{}{row("a")}, got.Data); diff != "" {
			t.Errorf("data: -want +got:\n%s", diff)
		}
		if got.ResultTruncated == nil || *got.ResultTruncated != armresourcegraph.ResultTruncatedTrue {
			t.Errorf("want ResultTruncatedTrue, got %v", got.ResultTruncated)
		}
	})

	t.Run("SecondPageError", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(to.Ptr("t1"), nil, row("a"))}, errOnCall: 2}
		_, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err == nil || !strings.Contains(err.Error(), "failed to finish the request") {
			t.Errorf("want failed to finish the request, got %v", err)
		}
		if len(c.reqs) != 2 {
			t.Errorf("want 2 calls, got %d", len(c.reqs))
		}
	})

	t.Run("TruncatedAggregated", func(t *testing.T) {
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{
			page(to.Ptr("t1"), to.Ptr(armresourcegraph.ResultTruncatedFalse), row("a")),
			page(nil, to.Ptr(armresourcegraph.ResultTruncatedTrue), row("b")),
		}}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if got.ResultTruncated == nil || *got.ResultTruncated != armresourcegraph.ResultTruncatedTrue {
			t.Errorf("want ResultTruncatedTrue, got %v", got.ResultTruncated)
		}
	})

	t.Run("MapDataUnchanged", func(t *testing.T) {
		mapData := map[string]interface{}{"count": 5}
		resp := armresourcegraph.ClientResourcesResponse{QueryResponse: armresourcegraph.QueryResponse{Data: mapData, SkipToken: to.Ptr("t1")}}
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{resp}}
		got, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(mapData, got.Data); diff != "" {
			t.Errorf("data: -want +got:\n%s", diff)
		}
		if len(c.reqs) != 1 {
			t.Errorf("want 1 call, got %d", len(c.reqs))
		}
	})

	t.Run("ClientError", func(t *testing.T) {
		c := &fakeResourcesClient{err: errors.New("boom")}
		_, err := newAQ(c).azQuery(context.Background(), spCreds, baseIn(), logging.NewNopLogger())
		if err == nil || !strings.Contains(err.Error(), "failed to finish the request") {
			t.Errorf("want failed to finish the request, got %v", err)
		}
	})

	t.Run("WorkloadIdentitySingleEntry", func(t *testing.T) {
		tokenFile := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(tokenFile, []byte("token"), 0o600); err != nil {
			t.Fatal(err)
		}
		c := &fakeResourcesClient{pages: []armresourcegraph.ClientResourcesResponse{page(nil, nil, row("a"))}}
		in := baseIn()
		in.Identity = &v1beta1.Identity{Type: v1beta1.IdentityTypeAzureWorkloadIdentityCredentials}
		creds := map[string]string{WorkloadIdentityCredentialPath: tokenFile, TenantID: "tenant", ClientID: "id"}
		if _, err := newAQ(c).azQuery(context.Background(), creds, in, logging.NewNopLogger()); err != nil {
			t.Fatal(err)
		}
		if len(c.reqs) != 1 {
			t.Errorf("want 1 call, got %d", len(c.reqs))
		}
	})
}
