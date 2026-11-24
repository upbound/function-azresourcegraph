# function-azresourcegraph end-to-end tests

This directory is a small Upbound project used only to run **real** end-to-end
tests for `function-azresourcegraph`: `up test run tests/* --e2e` installs it on a
throwaway control plane, together with the function built from the pull request,
and checks that the function answers a live Azure Resource Graph query. It is never
published to production.

## Layout

```
e2e/
├── upbound.yaml                 # test configuration; depends on the function under test (pinned by CI)
├── apis/xqueries/               # XRD + Composition that call the function
├── functions/.gitkeep           # must exist for `up`
└── tests/
    └── e2etest-queries/
        └── test.yaml            # E2ETest: default DRC audience + identity Secret, applies an XQuery, waits for Ready
```

The query lists the subscriptions the e2e identity can read. It reads no resource
and creates nothing. Any sign-in or query error makes the function return a fatal
result, so the XR never becomes `Ready` and the test fails.

## Credentials (no Azure secrets in CI)

The function signs in with Azure workload identity, using the Upbound OIDC token
of its own pod:

- `test.yaml` patches the control plane's `default` `DeploymentRuntimeConfig` with
  `proidc.cloud-spaces.upbound.io/audience: api://AzureADTokenExchange`. Upbound
  then mounts a token for that audience at
  `/var/run/secrets/upbound.io/provider/token` (function pods too).
- `test.yaml` also creates the `azure-e2e-identity` Secret the Composition passes
  to the function. It holds only non-secret values: the Entra app (client) ID, the
  tenant ID and the token path.

### Entra setup

App registration `solutions-e2e-function` (client ID
`b7b332bc-677c-4bae-a3ba-0aa7e6b2f24a`) with **Reader** on one subscription, and a
federated credential:

| Field    | Value |
|----------|-------|
| Issuer   | `https://proidc.upbound.io` |
| Audience | `api://AzureADTokenExchange` |
| Subject  | `mcp:solutions/function-azresourcegraph-e2e-wrapper-uptest-e2e-azresourcegraph:function:solutions-function-azresourcegraph` |

Entra matches the subject exactly. It is `mcp:<org>/<control plane>:function:<Function name>`:
`up test` names the control plane `<project>-uptest-<test>`, and a function
installed as a dependency is named `<org>-<repo>`. Renaming the project, the test
or the org changes the subject, and the federated credential must follow.

## Two repos, one function

The function dev build is pushed to `solutions/<repo>` and the wrapper project to
**`solutions/<repo>-e2e-wrapper`**. They must be separate: Crossplane's package
lock keys dependency-graph nodes by source repo, so a project and a function
sharing one repo collide (`node ... already exists`). The `<repo>-e2e-wrapper`
repo is granted to the e2e robot in `solutions-gitops-prod`.

## What runs in CI

On a pull request labelled `run-e2e-tests`, `.github/workflows/e2e.yaml` (synced
from `upbound/sa-up`):

1. builds the function package from the PR head and pushes it to `solutions/<repo>`
   at a dev tag,
2. pins that tag in `upbound.yaml` (`@FUNCTION_VERSION@`),
3. switches to the e2e Spaces control plane group, and
4. runs `up test run tests/* --e2e`, pushing the wrapper project to
   `solutions/<repo>-e2e-wrapper`.
