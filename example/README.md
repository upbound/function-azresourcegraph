# Example manifests

This directory contains a collection of practical examples to demonstrate the functionality. Each example is organized into a directory with a self-descriptive name.

## Usage

To render a specific example, navigate to its directory and run the `make` command.

Each example provides a unique `composition.yaml` file that highlights a specific function usage scenario.

The Makefile in the examples directory provides a simple `render` target to
streamline rendering Crossplane compositions.

To enable a successful query, update `secrets/azure-creds.yaml` with
your valid Azure credentials.

## Periodic Query Examples

The function now supports periodic queries to handle Azure Resource Graph API throttling and partial data scenarios. Use the `queryIntervalMinutes` parameter to limit query frequency:

- **[periodic-query-example/](./periodic-query-example/)**: Basic periodic query setup with 15-minute intervals
- **[throttling-prevention-example/](./throttling-prevention-example/)**: Advanced example showing multiple queries with different intervals to prevent throttling

### Key Features

- **Throttling Prevention**: Automatically limits query frequency to avoid Azure API rate limits
- **Partial Data Handling**: Allows queries to run periodically until complete data is obtained
- **Flexible Intervals**: Configure different intervals for different queries based on complexity
- **Backward Compatible**: Existing compositions work unchanged

## Example

For instance, the static-query-to-context-field directory demonstrates how to use a static query to populate a specific context field.

```shell
$ cd queryref-from-environment
$ make
crossplane render ../xr.yaml composition.yaml ./functions.yaml --function-credentials=../secrets/azure-creds.yaml --extra-resources=envconfig.yaml  -rc
---
apiVersion: example.crossplane.io/v1
kind: XR
metadata:
  name: example-xr
status:
  conditions:
  - lastTransitionTime: "2024-01-01T00:00:00Z"
    reason: Available
    status: "True"
    type: Ready
---
apiVersion: render.crossplane.io/v1beta1
kind: Result
message: 'Query: "Resources|count"'
severity: SEVERITY_NORMAL
step: query-azresourcegraph
---
apiVersion: render.crossplane.io/v1beta1
fields:
  apiextensions.crossplane.io/environment:
    apiVersion: internal.crossplane.io/v1alpha1
    azResourceGraphQuery: Resources|count
    kind: Environment
  azResourceGraphQueryResult:
  - Count: 204
kind: Context
```

Explore the examples to better understand various use cases and integrations!
