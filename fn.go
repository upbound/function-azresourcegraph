package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/upboundcare/function-azresourcegraph/input/v1beta1"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"
)

// AzureQueryInterface defines the methods required for querying Azure resources.
type AzureQueryInterface interface {
	azQuery(ctx context.Context, azureCreds map[string]string, in *v1beta1.Input) (armresourcegraph.ClientResourcesResponse, error)
}

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	azureQuery AzureQueryInterface

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(ctx context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	in := &v1beta1.Input{}
	if err := request.GetInput(req, in); err != nil {
		// You can set a custom status condition on the claim. This allows you to
		// communicate with the user. See the link below for status condition
		// guidance.
		// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong.").
			TargetCompositeAndClaim()

		// You can emit an event regarding the claim. This allows you to communicate
		// with the user. Note that events should be used sparingly and are subject
		// to throttling; see the issue below for more information.
		// https://github.com/crossplane/crossplane/issues/5802
		response.Warning(rsp, errors.New("something went wrong")).
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input from %T", req))
		return rsp, nil
	}

	azureCreds, err := getCreds(req)
	if err != nil {
		response.Fatal(rsp, err)
		return rsp, nil
	}

	if f.azureQuery == nil {
		f.azureQuery = &AzureQuery{}
	}

	switch {
	case in.QueryRef == nil:
	case strings.HasPrefix(*in.QueryRef, "status."):
		// The composite resource that actually exists.
		oxr, err := request.GetObservedCompositeResource(req)
		if err != nil {
			response.Fatal(rsp, errors.Wrap(err, "cannot get observed composite resource"))
			return rsp, nil
		}
		xrStatus := make(map[string]interface{})
		err = oxr.Resource.GetValueInto("status", &xrStatus)
		if err != nil {
			response.Fatal(rsp, errors.Wrap(err, "cannot get XR status"))
			return rsp, nil
		}
		if queryFromXRStatus, ok := GetNestedContextKey(xrStatus, strings.TrimPrefix(*in.QueryRef, "status.")); ok {
			in.Query = queryFromXRStatus
		}
	case strings.HasPrefix(*in.QueryRef, "context."):
		functionContext := req.GetContext().AsMap()
		if queryFromContext, ok := GetNestedContextKey(functionContext, strings.TrimPrefix(*in.QueryRef, "context.")); ok {
			in.Query = queryFromContext
		}
	default:
		response.Fatal(rsp, errors.Errorf("Unrecognized QueryRef field: %s", *in.QueryRef))
		return rsp, nil
	}

	if in.Query == "" {
		response.Fatal(rsp, errors.New("Query is empty"))
		f.log.Info("FAILURE: ", "query is empty", in.Query)
		return rsp, nil
	}

	results, err := f.azureQuery.azQuery(ctx, azureCreds, in)
	if err != nil {
		response.Fatal(rsp, err)
		f.log.Info("FAILURE: ", "failure", fmt.Sprint(err))
		return rsp, nil
	}
	// Print the obtained query results
	f.log.Info("Query:", "query", in.Query)
	f.log.Info("Results:", "results", fmt.Sprint(results.Data))
	response.Normalf(rsp, "Query: %q", in.Query)

	switch {
	case strings.HasPrefix(in.Target, "status."):
		// The composite resource that actually exists.
		oxr, err := request.GetObservedCompositeResource(req)
		if err != nil {
			response.Fatal(rsp, errors.Wrap(err, "cannot get observed composite resource"))
			return rsp, nil
		}
		// The composite resource desired by previous functions in the pipeline.
		dxr, err := request.GetDesiredCompositeResource(req)
		if err != nil {
			response.Fatal(rsp, errors.Wrap(err, "cannot get desired composite resource"))
			return rsp, nil
		}
		dxr.Resource.SetAPIVersion(oxr.Resource.GetAPIVersion())
		dxr.Resource.SetKind(oxr.Resource.GetKind())
		TargetXRStatusField := in.Target
		err = dxr.Resource.SetValue(TargetXRStatusField, &results.Data)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot set field %s to %s for %s", TargetXRStatusField, results.Data, dxr.Resource.GetKind()))
			return rsp, nil
		}
		if err := response.SetDesiredCompositeResource(rsp, dxr); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composite resource in %T", rsp))
			return rsp, nil
		}
	case strings.HasPrefix(in.Target, "context."):
		contextField := strings.TrimPrefix(in.Target, "context.")
		data, err := structpb.NewValue(results.Data)
		if err != nil {
			response.Fatal(rsp, errors.Wrap(err, "cannot convert results data to structpb.Value"))
			return rsp, nil
		}
		f.log.Debug("Updating Composition environment", "key", contextField, "data", &results.Data)
		response.SetContextKey(rsp, contextField, data)
	default:
		response.Fatal(rsp, errors.Errorf("Unrecognized target field: %s", in.Target))
		return rsp, nil
	}

	// You can set a custom status condition on the claim. This allows you to
	// communicate with the user. See the link below for status condition
	// guidance.
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").
		TargetCompositeAndClaim()

	return rsp, nil
}

func getCreds(req *fnv1.RunFunctionRequest) (map[string]string, error) {
	var azureCreds map[string]string
	rawCreds := req.GetCredentials()

	if credsData, ok := rawCreds["azure-creds"]; ok {
		credsData := credsData.GetCredentialData().GetData()
		if credsJSON, ok := credsData["credentials"]; ok {
			err := json.Unmarshal(credsJSON, &azureCreds)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse json credentials")
			}
		}
	} else {
		return nil, errors.New("failed to get azure-creds credentials")
	}

	return azureCreds, nil
}

// AzureQuery is a concrete implementation of the AzureQueryInterface
// that interacts with Azure Resource Graph API.
type AzureQuery struct{}

func (a *AzureQuery) azQuery(ctx context.Context, azureCreds map[string]string, in *v1beta1.Input) (armresourcegraph.ClientResourcesResponse, error) {
	tenantID := azureCreds["tenantId"]
	clientID := azureCreds["clientId"]
	clientSecret := azureCreds["clientSecret"]
	subscriptionID := azureCreds["subscriptionId"]

	// To configure DefaultAzureCredential to authenticate a user-assigned managed identity,
	// set the environment variable AZURE_CLIENT_ID to the identity's client ID.

	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		return armresourcegraph.ClientResourcesResponse{}, errors.Wrap(err, "failed to obtain credentials")
	}

	// Create and authorize a ResourceGraph client
	client, err := armresourcegraph.NewClient(cred, nil)
	if err != nil {
		return armresourcegraph.ClientResourcesResponse{}, errors.Wrap(err, "failed to create client")
	}

	queryRequest := armresourcegraph.QueryRequest{
		Query: to.Ptr(in.Query),
	}

	if len(subscriptionID) > 0 {
		queryRequest.Subscriptions = []*string{to.Ptr(subscriptionID)}
	}

	if len(in.ManagementGroups) > 0 {
		queryRequest.ManagementGroups = in.ManagementGroups
	}

	// Create the query request, Run the query and get the results.
	results, err := client.Resources(ctx, queryRequest, nil)
	if err != nil {
		return armresourcegraph.ClientResourcesResponse{}, errors.Wrap(err, "failed to finish the request")
	}
	return results, nil
}

// GetNestedContextKey retrieves a nested string value from a map using dot notation keys.
func GetNestedContextKey(context map[string]interface{}, key string) (string, bool) {
	// Regular expression to extract keys, supporting both dot and bracket notation
	keyRegex := regexp.MustCompile(`\[([^\[\]]+)\]|([^.\[\]]+)`)
	matches := keyRegex.FindAllStringSubmatch(key, -1)

	// Extract all keys in the proper order
	var keys []string
	for _, match := range matches {
		if match[1] != "" {
			keys = append(keys, match[1]) // Bracket key
		} else if match[2] != "" {
			keys = append(keys, match[2]) // Dot key
		}
	}
	currentValue := interface{}(context)

	for _, k := range keys {
		// Check if the current value is a map
		if nestedMap, ok := currentValue.(map[string]interface{}); ok {
			// Get the next value in the nested map
			if nextValue, exists := nestedMap[k]; exists {
				currentValue = nextValue
			} else {
				return "", false
			}
		} else {
			return "", false
		}
	}

	// Convert the final value to a string
	if result, ok := currentValue.(string); ok {
		return result, true
	}
	return "", false
}
