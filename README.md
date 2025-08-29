# function-azresourcegraph
[![CI](https://github.com/upbound/function-azresourcegraph/actions/workflows/ci.yml/badge.svg)](https://github.com/upbound/function-azresourcegraph/actions/workflows/ci.yml)

A function to query [Azure Resource Graph][azresourcegraph]

## Usage

See the [examples][examples] for a variety of practical and testable use cases demonstrating this Function.

Example pipeline step:

```yaml
  pipeline:
  - step: query-azresourcegraph
    functionRef:
      name: function-azresourcegraph
    input:
      apiVersion: azresourcegraph.fn.crossplane.io/v1alpha1
      kind: Input
      query: "Resources | project name, location, type, id| where type =~ 'Microsoft.Compute/virtualMachines' | order by name desc"
      target: "status.azResourceGraphQueryResult"
    credentials:
      - name: azure-creds
        source: Secret
        secretRef:
          namespace: upbound-system
          name: azure-account-creds
```

The Azure Credentials Secret structure is fully compatible with the standard
[Azure Official Provider][azop]

Example XR status after e2e query:

```yaml
apiVersion: example.crossplane.io/v1
kind: XR
metadata:
...
status:
  azResourceGraphQueryResult:
  - id: /subscriptions/f403a412-959c-4214-8c4d-ad5598f149cc/resourceGroups/us-vm-zxqnj-s2jdb/providers/Microsoft.Compute/virtualMachines/us-vm-zxqnj-2h59v
    location: centralus
    name: us-vm-zxqnj-2h59v
    type: microsoft.compute/virtualmachines
  - id: /subscriptions/f403a412-959c-4214-8c4d-ad5598f149cc/resourceGroups/us-vm-lzbpt-tdv2h/providers/Microsoft.Compute/virtualMachines/us-vm-lzbpt-fgcds
    location: centralus
    name: us-vm-lzbpt-fgcds
    type: microsoft.compute/virtualmachines
```

### QueryRef

Rather than specifying a direct query string as shown in the example above,
the function allows referencing a query from any arbitrary field within the Context or Status.

#### Context Query Reference

* Simple context field reference
```yaml
      queryRef: "context.azResourceGraphQuery"
```

* Get data from Environment
```yaml
      queryRef: "context.[apiextensions.crossplane.io/environment].azResourceGraphQuery"
```

#### XR Status Query Reference

* Simple XR Status field reference
```yaml
      queryRef: "status.azResourceGraphQuery"
```

* Get data from nested field in XR status. Use brackets if key contains dots.
```yaml
      queryRef: "status.[fancy.key.with.dots].azResourceGraphQuery"
```

### Targets

Function supports publishing Query Results to different locations.

#### Context Target

* Simple Context field target
```yaml
      target: "context.azResourceGraphQueryResult"
```

* Put results into Environment key
```yaml
      target: "context.[apiextensions.crossplane.io/environment].azResourceGraphQuery"
```

#### XR Status Target

* Simple XR status field target
```yaml
      target: "status.azResourceGraphQueryResult"
```

* Put query results to nested field under XR status. Use brackets if key contains dots
```yaml
      target: "status.[fancy.key.with.dots].azResourceGraphQueryResult"
```

## Mitigating Azure API throttling

If you encounter Azure API throttling, you can reduce the number of queries
using the optional `skipQueryWhenTargetHasData` flag:

```yaml
  - step: query-azresourcegraph
    functionRef:
      name: function-azresourcegraph
    input:
      apiVersion: azresourcegraph.fn.crossplane.io/v1beta1
      kind: Input
      query: "Resources | project name, location, type, id| where type =~ 'Microsoft.Compute/virtualMachines' | order by name desc"
      target: "status.azResourceGraphQueryResult"
      skipQueryWhenTargetHasData: true # Optional: Set to true to skip query if target already contains data
```

Use this option carefully, as it may lead to stale query results over time.

## Explicit Subscriptions scope

It is possible to specify explicit subscriptions scope and override the one that
is coming from credentials

```yaml
      kind: Input
      query: "Resources | project name, location, type, id| where type =~ 'Microsoft.Compute/virtualMachines' | order by name desc"
      subscriptions:
        - 00000000-0000-0000-0000-000000000001
        - 00000000-0000-0000-0000-000000000002
      target: "status.azResourceGraphQueryResult"
```

There is also possible to use references from status and context.


```yaml
subscriptionsRef: status.subscriptions
```

```yaml
subscriptionsRef: "context.[apiextensions.crossplane.io/environment].subscriptions"
```

## Round-robin Service Principal Authentication

To further mitigate Azure ARM throttling, you can now use multiple service principals with automatic round-robin selection. This distributes load across multiple identities and reduces the likelihood of hitting rate limits.

### Multiple Service Principals

Configure multiple service principals in your credentials secret as a JSON array:

```yaml
credentials:
  - name: azure-creds
    source: Secret
    secretRef:
      namespace: upbound-system
      name: azure-account-creds
```

With the secret containing:

```json
[
  {
    "subscriptionId": "sub-id",
    "tenantId": "tenant-id",
    "clientId": "client-1",
    "clientSecret": "secret-1"
  },
  {
    "subscriptionId": "sub-id",
    "tenantId": "tenant-id",
    "clientId": "client-2",
    "clientSecret": "secret-2"
  }
]
```

### How Round-Robin Works

- Each reconciliation cycle automatically selects the next service principal
- Load is distributed evenly across all configured service principals
- The function cycles through: SP-0 → SP-1 → SP-2 → SP-0 → SP-1 → SP-2...
- Single service principal format is still supported for backward compatibility

### Benefits

- **Prevents Throttling**: Distributes API calls across multiple service principals
- **Load Balancing**: Evenly distributes workload
- **Easy Scaling**: Add more service principals without code changes
- **Backward Compatible**: Existing single service principal configurations work unchanged

[azresourcegraph]: https://learn.microsoft.com/en-us/azure/governance/resource-graph/
[azop]: https://marketplace.upbound.io/providers/upbound/provider-family-azure/latest
[examples]: ./example

## Workload Identity Authentication
AKS cluster needs to have workload identity enabled.
The managed identity needs to have the Federated Identity Credential created: https://azure.github.io/azure-workload-identity/docs/topics/federated-identity-credential.html.

&#x26a0;&#xfe0f;
Does not support Multiple Service Principals with Round-Robin

### Credentials secret:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: azure-account-creds
  namespace: crossplane-system
type: Opaque
stringData:
  credentials: |
    {
      "clientId": "your-client-id", # optional, defaults to AZURE_CLIENT_ID injected by Azure Workload Identity
      "tenantId": "your-tenant-id", # optional, defaults to AZURE_TENANT_ID injected by Azure Workload Identity
      "subscriptionId": "your-subscription-id" # optional, if both subscriptionId and Explicit Subscriptions scope is not defined defaults to tenant-scope search
      "federatedTokenFile": "/var/run/secrets/azure/tokens/azure-identity-token"
    }
```

#### Function
```yaml
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: upbound-function-azresourcegraph
spec:
  package: xpkg.upbound.io/upbound/function-azresourcegraph:v0.10.0
  runtimeConfigRef:
    apiVersion: pkg.crossplane.io/v1beta1
    kind: DeploymentRuntimeConfig
    name: upbound-function-azresourcegraph
```

#### DeploymentRuntimeConfig
```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: upbound-function-azresourcegraph
spec: 
  deploymentTemplate:
    spec:
      selector:
        matchLabels:
          azure.workload.identity/use: "true"
          pkg.crossplane.io/function: "upbound-function-azresourcegraph"
      template:
        metadata:
          labels:
            azure.workload.identity/use: "true"
            pkg.crossplane.io/function: "upbound-function-azresourcegraph"
        spec:
          containers:
          - name: package-runtime
            volumeMounts:
            - mountPath: /var/run/secrets/azure/tokens
              name: azure-identity-token
              readOnly: true
          serviceAccountName: "upbound-function-azresourcegraph"
          volumes:
          - name: azure-identity-token
            projected:
              sources:
              - serviceAccountToken:
                  audience: api://AzureADTokenExchange
                  expirationSeconds: 3600
                  path: azure-identity-token
  serviceAccountTemplate:
    metadata:
      annotations:
        azure.workload.identity/client-id: "your-client-id"
      name: "upbound-function-azresourcegraph"
```

## Using Different Credentials

### Using ServicePrincipal credentials

#### Explicitly
```yaml
apiVersion: azresourcegraph.fn.crossplane.io/v1beta1
kind: Input
identity:
  type: AzureServicePrincipalCredentials
```

#### Default
```yaml
apiVersion: azresourcegraph.fn.crossplane.io/v1beta1
kind: Input
```

### Using Workload Identity Credentials
```yaml
apiVersion: azresourcegraph.fn.crossplane.io/v1beta1
kind: Input
identity:
  type: AzureWorkloadIdentityCredentials
```
