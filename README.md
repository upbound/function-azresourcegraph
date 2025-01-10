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
the function allows referencing a query from any arbitrary string within the Context or Status.

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


[azresourcegraph]: https://learn.microsoft.com/en-us/azure/governance/resource-graph/
[azop]: https://marketplace.upbound.io/providers/upbound/provider-family-azure/latest
[examples]: ./example
