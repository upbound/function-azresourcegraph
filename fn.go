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
	"github.com/upbound/function-azresourcegraph/input/v1beta1"
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

	// Parse input and get credentials
	in, azureCreds, err := f.parseInputAndCredentials(req, rsp)
	if err != nil {
		return rsp, nil //nolint:nilerr // errors are handled in rsp. We should not error main function and proceed with reconciliation
	}

	// Get query from reference if specified
	if err := f.resolveQuery(req, in, rsp); err != nil {
		return rsp, nil //nolint:nilerr // errors are handled in rsp. We should not error main function and proceed with reconciliation
	}

	// Check if query is empty
	if in.Query == "" {
		response.Warning(rsp, errors.New("Query is empty"))
		f.log.Info("WARNING: ", "query is empty", in.Query)
		return rsp, nil
	}

	// Check if target is valid
	if !f.isValidTarget(in.Target) {
		response.Fatal(rsp, errors.Errorf("Unrecognized target field: %s", in.Target))
		return rsp, nil
	}

	// Check if we should skip the query
	if f.shouldSkipQuery(req, in, rsp) {
		return rsp, nil
	}

	// Execute the query
	results, err := f.executeQuery(ctx, azureCreds, in, rsp)
	if err != nil {
		return rsp, nil //nolint:nilerr // errors are handled in rsp. We should not error main function and proceed with reconciliation
	}

	// Process the results
	if err := f.processResults(req, in, results, rsp); err != nil {
		return rsp, nil //nolint:nilerr // errors are handled in rsp. We should not error main function and proceed with reconciliation
	}

	// Set success condition
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").
		TargetCompositeAndClaim()

	return rsp, nil
}

// parseInputAndCredentials parses the input and gets the credentials.
func (f *Function) parseInputAndCredentials(req *fnv1.RunFunctionRequest, rsp *fnv1.RunFunctionResponse) (*v1beta1.Input, map[string]string, error) {
	in := &v1beta1.Input{}
	if err := request.GetInput(req, in); err != nil {
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong.").
			TargetCompositeAndClaim()

		response.Warning(rsp, errors.New("something went wrong")).
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input from %T", req))
		return nil, nil, err
	}

	azureCreds, err := getCreds(req)
	if err != nil {
		response.Fatal(rsp, err)
		return nil, nil, err
	}

	if f.azureQuery == nil {
		f.azureQuery = &AzureQuery{}
	}

	return in, azureCreds, nil
}

// resolveQuery resolves the query from a reference if specified.
func (f *Function) resolveQuery(req *fnv1.RunFunctionRequest, in *v1beta1.Input, rsp *fnv1.RunFunctionResponse) error {
	switch {
	case in.QueryRef == nil:
		return nil
	case strings.HasPrefix(*in.QueryRef, "status."):
		if err := getQueryFromStatus(req, in); err != nil {
			response.Fatal(rsp, err)
			return err
		}
	case strings.HasPrefix(*in.QueryRef, "context."):
		functionContext := req.GetContext().AsMap()
		if queryFromContext, ok := GetNestedKey(functionContext, strings.TrimPrefix(*in.QueryRef, "context.")); ok {
			in.Query = queryFromContext
		}
	default:
		response.Fatal(rsp, errors.Errorf("Unrecognized QueryRef field: %s", *in.QueryRef))
		return errors.New("unrecognized QueryRef field")
	}
	return nil
}

// isValidTarget checks if the target is valid
func (f *Function) isValidTarget(target string) bool {
	return strings.HasPrefix(target, "status.") || strings.HasPrefix(target, "context.")
}

// shouldSkipQuery checks if the query should be skipped.
func (f *Function) shouldSkipQuery(req *fnv1.RunFunctionRequest, in *v1beta1.Input, rsp *fnv1.RunFunctionResponse) bool {
	// Determine if we should skip the query when target has data
	var shouldSkipQueryWhenTargetHasData = false // Default to false to ensure continuous reconciliation
	if in.SkipQueryWhenTargetHasData != nil {
		shouldSkipQueryWhenTargetHasData = *in.SkipQueryWhenTargetHasData
	}

	if !shouldSkipQueryWhenTargetHasData {
		return false
	}

	switch {
	case strings.HasPrefix(in.Target, "status."):
		return f.checkStatusTargetHasData(req, in, rsp)
	case strings.HasPrefix(in.Target, "context."):
		return f.checkContextTargetHasData(req, in, rsp)
	}

	return false
}

// checkStatusTargetHasData checks if the status target has data.
func (f *Function) checkStatusTargetHasData(req *fnv1.RunFunctionRequest, in *v1beta1.Input, rsp *fnv1.RunFunctionResponse) bool {
	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrap(err, "cannot get observed composite resource"))
		return true
	}

	xrStatus := make(map[string]interface{})
	err = oxr.Resource.GetValueInto("status", &xrStatus)
	if err == nil {
		// Check if the target field already has data
		statusField := strings.TrimPrefix(in.Target, "status.")
		if hasData, _ := targetHasData(xrStatus, statusField); hasData {
			f.log.Info("Target already has data, skipping query", "target", in.Target)

			// Set success condition and return
			response.ConditionTrue(rsp, "FunctionSkip", "SkippedQuery").
				WithMessage("Target already has data, skipped query to avoid throttling").
				TargetCompositeAndClaim()
			return true
		}
	}
	return false
}

// checkContextTargetHasData checks if the context target has data.
func (f *Function) checkContextTargetHasData(req *fnv1.RunFunctionRequest, in *v1beta1.Input, rsp *fnv1.RunFunctionResponse) bool {
	contextMap := req.GetContext().AsMap()
	contextField := strings.TrimPrefix(in.Target, "context.")
	if hasData, _ := targetHasData(contextMap, contextField); hasData {
		f.log.Info("Target already has data, skipping query", "target", in.Target)

		// Set success condition and return
		response.ConditionTrue(rsp, "FunctionSkip", "SkippedQuery").
			WithMessage("Target already has data, skipped query to avoid throttling").
			TargetCompositeAndClaim()
		return true
	}
	return false
}

// executeQuery executes the query.
func (f *Function) executeQuery(ctx context.Context, azureCreds map[string]string, in *v1beta1.Input, rsp *fnv1.RunFunctionResponse) (armresourcegraph.ClientResourcesResponse, error) {
	results, err := f.azureQuery.azQuery(ctx, azureCreds, in)
	if err != nil {
		response.Fatal(rsp, err)
		f.log.Info("FAILURE: ", "failure", fmt.Sprint(err))
		return armresourcegraph.ClientResourcesResponse{}, err
	}

	// Print the obtained query results
	f.log.Info("Query:", "query", in.Query)
	f.log.Info("Results:", "results", fmt.Sprint(results.Data))
	response.Normalf(rsp, "Query: %q", in.Query)

	return results, nil
}

// processResults processes the query results.
func (f *Function) processResults(req *fnv1.RunFunctionRequest, in *v1beta1.Input, results armresourcegraph.ClientResourcesResponse, rsp *fnv1.RunFunctionResponse) error {
	switch {
	case strings.HasPrefix(in.Target, "status."):
		err := putQueryResultToStatus(req, rsp, in, results, f)
		if err != nil {
			response.Fatal(rsp, err)
			return err
		}
	case strings.HasPrefix(in.Target, "context."):
		err := putQueryResultToContext(req, rsp, in, results, f)
		if err != nil {
			response.Fatal(rsp, err)
			return err
		}
	default:
		// This should never happen because we check for valid targets earlier
		response.Fatal(rsp, errors.Errorf("Unrecognized target field: %s", in.Target))
		return errors.New("unrecognized target field")
	}
	return nil
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

// ParseNestedKey enables the bracket and dot notation to key reference
func ParseNestedKey(key string) ([]string, error) {
	var parts []string
	// Regular expression to extract keys, supporting both dot and bracket notation
	regex := regexp.MustCompile(`\[([^\[\]]+)\]|([^.\[\]]+)`)
	matches := regex.FindAllStringSubmatch(key, -1)
	for _, match := range matches {
		if match[1] != "" {
			parts = append(parts, match[1]) // Bracket notation
		} else if match[2] != "" {
			parts = append(parts, match[2]) // Dot notation
		}
	}

	if len(parts) == 0 {
		return nil, errors.New("invalid key")
	}
	return parts, nil
}

// GetNestedKey retrieves a nested string value from a map using dot notation keys.
func GetNestedKey(context map[string]interface{}, key string) (string, bool) {
	parts, err := ParseNestedKey(key)
	if err != nil {
		return "", false
	}

	currentValue := interface{}(context)
	for _, k := range parts {
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

// SetNestedKey sets a value to a nested key from a map using dot notation keys.
func SetNestedKey(root map[string]interface{}, key string, value interface{}) error {
	parts, err := ParseNestedKey(key)
	if err != nil {
		return err
	}

	current := root
	for i, part := range parts {
		if i == len(parts)-1 {
			// Set the value at the final key
			current[part] = value
			return nil
		}

		// Traverse into nested maps or create them if they don't exist
		if next, exists := current[part]; exists {
			if nextMap, ok := next.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return fmt.Errorf("key %q exists but is not a map", part)
			}
		} else {
			// Create a new map if the path doesn't exist
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	return nil
}

func getQueryFromStatus(req *fnv1.RunFunctionRequest, in *v1beta1.Input) error {
	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		return errors.Wrap(err, "cannot get observed composite resource")
	}
	xrStatus := make(map[string]interface{})
	err = oxr.Resource.GetValueInto("status", &xrStatus)
	if err != nil {
		return errors.Wrap(err, "cannot get XR status")
	}
	if queryFromXRStatus, ok := GetNestedKey(xrStatus, strings.TrimPrefix(*in.QueryRef, "status.")); ok {
		in.Query = queryFromXRStatus
	}
	return nil
}

func putQueryResultToStatus(req *fnv1.RunFunctionRequest, rsp *fnv1.RunFunctionResponse, in *v1beta1.Input, results armresourcegraph.ClientResourcesResponse, f *Function) error {
	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		return errors.Wrap(err, "cannot get observed composite resource")
	}
	// The composite resource desired by previous functions in the pipeline.
	dxr, err := request.GetDesiredCompositeResource(req)
	if err != nil {
		return errors.Wrap(err, "cannot get desired composite resource")
	}
	xrStatus := make(map[string]interface{})
	// Use Desired XR from previous pipeline as the current status
	// Otherwise get status from Observed XR
	if dxr.Resource.GetKind() != "" {
		err = dxr.Resource.GetValueInto("status", &xrStatus)
		if err != nil {
			f.log.Debug("Cannot get status from XR")
		}
	} else {
		dxr.Resource.SetAPIVersion(oxr.Resource.GetAPIVersion())
		dxr.Resource.SetKind(oxr.Resource.GetKind())

		err = oxr.Resource.GetValueInto("status", &xrStatus)
		if err != nil {
			f.log.Debug("Cannot get status from XR")
		}
	}

	// Update the specific status field using the reusable function
	statusField := strings.TrimPrefix(in.Target, "status.")
	err = SetNestedKey(xrStatus, statusField, results.Data)
	if err != nil {
		return errors.Wrapf(err, "cannot set status field %s to %v", statusField, results.Data)
	}

	// Write the updated status field back into the composite resource
	if err := dxr.Resource.SetValue("status", xrStatus); err != nil {
		return errors.Wrap(err, "cannot write updated status back into composite resource")
	}

	// Save the updated desired composite resource
	if err := response.SetDesiredCompositeResource(rsp, dxr); err != nil {
		return errors.Wrapf(err, "cannot set desired composite resource in %T", rsp)
	}
	return nil
}

func putQueryResultToContext(req *fnv1.RunFunctionRequest, rsp *fnv1.RunFunctionResponse, in *v1beta1.Input, results armresourcegraph.ClientResourcesResponse, f *Function) error {

	contextField := strings.TrimPrefix(in.Target, "context.")
	data, err := structpb.NewValue(results.Data)
	if err != nil {
		return errors.Wrap(err, "cannot convert results data to structpb.Value")
	}

	// Convert existing context into a map[string]interface{}
	contextMap := req.GetContext().AsMap()

	err = SetNestedKey(contextMap, contextField, data.AsInterface())
	if err != nil {
		return errors.Wrap(err, "failed to update context key")
	}

	f.log.Debug("Updating Composition Pipeline Context", "key", contextField, "data", &results.Data)

	// Convert the updated context back into structpb.Struct
	updatedContext, err := structpb.NewStruct(contextMap)
	if err != nil {
		return errors.Wrap(err, "failed to serialize updated context")
	}

	// Set the updated context
	rsp.Context = updatedContext
	return nil
}

// targetHasData checks if a target field already has data
func targetHasData(data map[string]interface{}, key string) (bool, error) {
	parts, err := ParseNestedKey(key)
	if err != nil {
		return false, err
	}

	currentValue := interface{}(data)
	for _, k := range parts {
		// Check if the current value is a map
		if nestedMap, ok := currentValue.(map[string]interface{}); ok {
			// Get the next value in the nested map
			if nextValue, exists := nestedMap[k]; exists {
				currentValue = nextValue
			} else {
				// Key doesn't exist, so no data
				return false, nil
			}
		} else {
			// Not a map, so can't traverse further
			return false, nil
		}
	}

	// If we've reached here, the key exists
	// Check if it has meaningful data (not nil and not empty)
	if currentValue == nil {
		return false, nil
	}

	// Check for empty maps
	if nestedMap, ok := currentValue.(map[string]interface{}); ok {
		return len(nestedMap) > 0, nil
	}

	// Check for empty slices
	if slice, ok := currentValue.([]interface{}); ok {
		return len(slice) > 0, nil
	}

	// For strings, check if empty
	if str, ok := currentValue.(string); ok {
		return str != "", nil
	}

	// For other types (numbers, booleans), consider them as having data
	return true, nil
}
