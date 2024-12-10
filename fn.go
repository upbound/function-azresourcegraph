package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/upboundcare/function-azresourcegraph/input/v1beta1"
)

const TargetXRStatusField = "status.azResourceGraphQueryResult"

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
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

	// Get and print secrets(TODO: remove)
	for name, credential := range req.GetCredentials() {
		fmt.Printf("Name: %s, Value: %s\n", name, string(credential.GetCredentialData().Data["credentials"]))
	}
	var azureCreds map[string]string
	json.Unmarshal(req.GetCredentials()["azure-creds"].GetCredentialData().Data["credentials"], &azureCreds)

	tenantID := azureCreds["tenantId"]
	clientID := azureCreds["clientId"]
	clientSecret := azureCreds["clientSecret"]
	subscriptionID := azureCreds["subscriptionId"]

	// To configure DefaultAzureCredential to authenticate a user-assigned managed identity,
	// set the environment variable AZURE_CLIENT_ID to the identity's client ID.

	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		response.Fatal(rsp, errors.Wrap(err, "failed to obtain a credentials"))
		return rsp, nil
	}

	ctx := context.Background()
	// Create and authorize a ResourceGraph client
	client, err := armresourcegraph.NewClient(cred, nil)
	if err != nil {
		response.Fatal(rsp, errors.Wrap(err, "failed to create client"))
		return rsp, nil
	}

	// Create the query request, Run the query and get the results. Update the VM and subscriptionID details below.
	results, err := client.Resources(ctx,
		armresourcegraph.QueryRequest{
			Query: to.Ptr("Resources | project name, location, type, id| where type =~ 'Microsoft.Compute/virtualMachines' | order by name desc"),
			Subscriptions: []*string{
				to.Ptr(subscriptionID)},
		},
		nil)
	if err != nil {
		response.Fatal(rsp, errors.Wrap(err, "failed to finish the request"))
		return rsp, nil
	} else {
		// Print the obtained query results
		fmt.Printf("Resources found: " + strconv.FormatInt(*results.TotalRecords, 10) + "\n")
		fmt.Printf("Results: " + fmt.Sprint(results.Data) + "\n")
	}

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
	// The composed resources desired by any previous Functions in the pipeline.
	desired, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired composed resources from %T", req))
		return rsp, nil
	}

	err = dxr.Resource.SetValue(TargetXRStatusField, results.Data)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set field %s to %s for %s", TargetXRStatusField, results.Data, dxr.Resource.GetKind()))
		return rsp, nil
	}

	if err := response.SetDesiredCompositeResource(rsp, dxr); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composite resource in %T", rsp))
		return rsp, nil
	}

	if err := response.SetDesiredComposedResources(rsp, desired); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composed resources in %T", rsp))
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
