# MIRA agent rebuild playbook

Instructions for an AI agent recreating MIRA in a new environment. Read the
[infrastructure guide](INFRASTRUCTURE.md) first. Use its deployment input contract,
not values from a previous deployment, Git history, or the agent's current Azure
and Kubernetes contexts.

This is a gated workflow, not an unattended install script. A request to read,
redact, or explain these documents is not permission to execute deployment steps.
Do not change an existing environment, rotate credentials, or connect to radio
without explicit authorization.

## 1. Collect inputs and inspect the selected source

Confirm the target tenant/subscription, regions, names, resource ownership, budget,
network policy, registry/cluster choice, Realtime model, and secret delivery method.
Confirm the source revision and whether an operator agent, Fabric, Search, Redis,
workplace data, hosted runtime, AI gateway, and governance pipeline are required.
Record skipped components explicitly.

Use a clean checkout or worktree for target-specific changes. Preserve unrelated
local work. Do not reset, stash, or broadly stage someone else's changes.

Read these component sources before planning commands:

| Source | What to establish |
|---|---|
| [Gateway setup](../services/mira-gateway/README.md) and [configuration](../services/mira-gateway/internal/config/config.go) | Native build dependencies, required settings, route precedence, defaults, and available health endpoints. |
| [Agent source directories](../services/operator-agent/src/) | Selected implementation, dependency manifest, model client, data tools, entry point, and health behavior. |
| [Hosted deployment helper](../services/operator-agent/deploy_hosted_agent.py) | If selected, inspect its inputs and side effects before running it. Creation, traffic routing, and test calls require approval. |
| [Governance tools](../ontology-governance/README.md) | Reader/publisher format compatibility, workflow location, and deployment dependencies. |

Inspect selected manifests and source for fixed tenant, endpoint, identity,
downstream-agent, team, or workspace values. Configuration replacement alone may
not fix hardcoded application dependencies. Prepare and validate a scoped change
for the new target, or report the implementation blocked. Do not imply that
external specialist agents or reviewed content are created by this repository.

Check current Azure service support, model/API compatibility, available SKUs,
quota, and relevant CLI command interfaces. Do not carry forward a fixed model
version, Kubernetes version, or capacity from historical examples.

## 2. Obtain plan approval

Present the proposed resource graph, create-versus-reuse decisions, estimated cost,
data boundaries, identity grants, validation strategy, and rollback/cleanup scope.
Use symbolic names in any public output. Keep resolved environment identifiers
in the operator's private deployment record.

Approval must identify the target and permitted changes. Treat account/channel
activation and test transmissions as separate later approvals. If permissions,
external services, quota, or credentials are missing, stop the affected step and
report the blocker instead of substituting another environment.

The command examples below assume Bash, Azure CLI, and operator-supplied variables.
They are fragments for the approved plan, not a complete provisioning script.
Never paste literal placeholder values into a cloud command.

## 3. Verify target and provision the foundation

Verify the approved subscription explicitly without changing shared CLI defaults:

```bash
set -euo pipefail
: "${AZURE_SUBSCRIPTION_ID:?Set the approved target subscription}"
: "${AZURE_TENANT_ID:?Set the approved target tenant}"
actual_tenant="$(az account show --subscription "$AZURE_SUBSCRIPTION_ID" \
  --query tenantId -o tsv)"
if [ "$actual_tenant" != "$AZURE_TENANT_ID" ]; then
  printf '%s\n' 'Target tenant mismatch; no provisioning permitted.' >&2
  exit 1
fi
```

Pass the explicit subscription to every subscription-scoped Azure command.
Create the approved resource groups before resources within them. Use reviewed
infrastructure definitions where available, after removing target-specific
assumptions. Otherwise prepare parameterized commands using current CLI help.
Preview the changes where supported; do not silently deploy or adopt unrelated
resources.

Provision in dependency order:

1. Registry and networking, then AKS with OIDC and Workload Identity enabled.
   Grant the kubelet the image-pull role required by the registry's permission mode.
2. Realtime model resource/deployment, plus the Foundry project and reasoning model
   required by the selected agent. Configure any approved AI gateway routes without
   conflating voice WebSocket traffic with HTTP inference or data-tool calls.
3. Selected data services: Fabric capacity/workspace/items, Search, and Redis.
   Establish data contracts and approved content before enabling their tools.
4. Monitoring resources needed by the selected workloads.
5. Runtime and publication identities, federated credentials, and required access.

Keep the gateway inactive throughout foundation setup. Do not load credentials
from an old cluster to make the new deployment work.

## 4. Bind identities and authorize data access

Retrieve the cluster OIDC issuer and each managed identity's client ID and
principal/object ID from the resources just approved or created. Validate
namespace/service-account names before creating federation.

This illustrates one workload federation, after the cluster and identity exist:

```bash
set -euo pipefail
: "${AZURE_SUBSCRIPTION_ID:?Set the approved subscription}"
: "${AKS_RESOURCE_GROUP:?Set the approved cluster resource group}"
: "${AKS_NAME:?Set the approved cluster name}"
: "${IDENTITY_RESOURCE_GROUP:?Set the identity resource group}"
: "${IDENTITY_NAME:?Set the workload identity name}"
: "${FEDERATION_NAME:?Set the credential name}"
: "${NAMESPACE:?Set the workload namespace}"
: "${SERVICE_ACCOUNT:?Set the workload service account}"
OIDC_ISSUER="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" \
  --resource-group "$AKS_RESOURCE_GROUP" --name "$AKS_NAME" \
  --query oidcIssuerProfile.issuerUrl -o tsv)"
: "${OIDC_ISSUER:?Cluster OIDC issuer is missing}"
az identity federated-credential create \
  --subscription "$AZURE_SUBSCRIPTION_ID" \
  --resource-group "$IDENTITY_RESOURCE_GROUP" \
  --identity-name "$IDENTITY_NAME" --name "$FEDERATION_NAME" \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT}" \
  --audience "api://AzureADTokenExchange"
```

For each identity, verify the exact issuer, subject, audience, service-account
client-ID annotation, and pod Workload Identity label. Assign only the permissions
needed for its own calls. Use principal IDs and explicit resource scopes for grants.
Verify propagation before escalating a permission.

Configure Redis access policies separately from resource-management roles.
Configure Fabric workspace/item permissions using supported APIs or the portal.
Validate the least-privilege role needed for the actual operation; do not grant
every identity workspace Member by default. Treat delegated workplace access as
a separate consent and authorization task, not a side effect of workload federation.

For a hosted agent, verify its actual identity after creation or a version change.
Reconcile permissions if that identity changes. Do not assume every version
receives a new identity or that required grants are implicit.

## 5. Prepare images, manifests, and private configuration

Run the selected implementation's local checks before building. Gateway native
audio support needs the production container build or the documented native
libraries; a stub-only local build does not prove radio audio will work.

An approved registry build can use this parameterized pattern:

```bash
set -euo pipefail
: "${AZURE_SUBSCRIPTION_ID:?Set the approved subscription}"
: "${ACR_NAME:?Set the approved registry}"
: "${IMAGE_TAG:?Set a unique release tag tied to the source revision}"
az acr build --subscription "$AZURE_SUBSCRIPTION_ID" \
  --registry "$ACR_NAME" --image "mira:${IMAGE_TAG}" \
  --file services/mira-gateway/Dockerfile services/mira-gateway
```

Build only the selected agent variant with its own Dockerfile and required build
context. Record the exact source revision and resulting registry digest. Deploy
digest-pinned images, not mutable `latest` tags.

Render a new target overlay. Replace registry, namespaces, service-account
annotations, endpoints, model deployments, data source references, and secret
references consistently. Review security contexts, writable mounts, resource
limits, probes, network policy, and the internal agent Service.

Set the gateway to **zero replicas** in the initial overlay. Do not start it merely
to check whether credentials work. There is no assumed receive-only deployment
mode, and another user's transmission could trigger a response.

Inject Zello credentials and any required API keys, connection strings, or delegated
token material through the approved secret mechanism. Do not use `--from-literal`
with secrets, print secret values, store them in tracked `.env` files, or include
them in rendered output. Confirm required key names and references without dumping
the secret objects. Never copy an issuer private key into the application image.

## 6. Deploy into an isolated Kubernetes context

Retrieve credentials into a newly created private directory, not the shared default
kubeconfig. This example creates no Kubernetes workload:

```bash
set -euo pipefail
: "${AZURE_SUBSCRIPTION_ID:?Set the approved subscription}"
: "${AKS_RESOURCE_GROUP:?Set the approved cluster resource group}"
: "${AKS_NAME:?Set the approved cluster name}"
umask 077
DEPLOYMENT_STATE_DIR="$(mktemp -d)"
KUBECONFIG_PATH="${DEPLOYMENT_STATE_DIR}/kubeconfig"
az aks get-credentials --subscription "$AZURE_SUBSCRIPTION_ID" \
  --resource-group "$AKS_RESOURCE_GROUP" --name "$AKS_NAME" \
  --file "$KUBECONFIG_PATH"
kubectl --kubeconfig "$KUBECONFIG_PATH" config current-context
```

Verify the API server and context against the approved cluster. Pass this explicit
kubeconfig to every subsequent `kubectl` command. If using a different shell,
restore the recorded path rather than falling back to the default context.
The file contains cluster access material: retain it privately and remove that
specific file and its empty temporary directory when the deployment task ends.

Validate the rendered, non-secret manifests with server-side dry-run and a
reviewed diff. A `kubectl diff` exit status of 1 means differences exist, not that
the command failed. Investigate other errors rather than ignoring them.
Apply only the approved target manifests, in order: namespaces and service accounts,
secret integration, configuration and Services, network policies, then workloads.
Keep the gateway at zero replicas.

If using a hosted-agent helper, inspect and gate any automatic model test or traffic
switch it performs. Do not run a helper whose side effects exceed the approved plan.

## 7. Configure governance if selected

Prepare reviewed root-level GitHub Actions workflows for the target repository.
The nested `ontology-governance/.github/workflows/` examples do not automatically
run in this monorepo. Adjust path filters, working directories, and the Fabric
Git integration folder.

Configure the approved GitHub OIDC federation and destination permissions.
Set workflow variables from target outputs, not copied identities or hostnames.
Do not grant subscription-wide roles merely to satisfy a login action that can
operate with data-plane-only permissions.

Run validation without credentials first. With publication approval, publish a
synthetic or approved schema/content release, then verify the consumer can load
the expected version. Check failure behavior for unavailable or incompatible
content. A successful workflow alone does not prove runtime activation.

## 8. Validate without radio, then request activation approval

Complete offline checks and, only with approval for model/data calls, a non-radio
smoke test using synthetic input. Verify authentication, tool routing, evidence,
timeouts, and explicit failure responses. Do not send private operational data
merely to test the deployment.

Before radio activation, require:

- Target, image digests, configuration, and identities match the approved plan.
- Agent health and required data access work, with unavailable sources reported
  explicitly rather than producing invented answers.
- Direct agent endpoints are not publicly exposed and access is restricted.
- Secret values and private data are absent from build output and diagnostics.
- Gateway remains at zero replicas; no Zello connection or transmission occurred.

Report the environment as **prepared, radio not activated** until the operator
approves activation. Approval must identify the account/channel, behavior, window,
and a way to stop the gateway. Scale only the approved deployment when authorized.
Then verify health/readiness and connection state using narrowly scoped,
privacy-filtered diagnostics.

A radio test requires explicit approval as well. Use an agreed non-sensitive
question on an authorized test channel; do not simulate emergency traffic.
Verify the actual spoken response and turn handling. Never claim an end-to-end
radio test passed based only on pod status or an HTTP response.

## 9. Handoff and cleanup

Privately record the source revision, image digests, target resource references,
selected features, identity scopes, configuration references, verification results,
known limitations, and exact activation state. Distinguish created, reused,
skipped, blocked, and tested components. Give the operator a scoped stop/rollback
procedure that does not delete shared resources.

Remove only task-created temporary artifacts containing access material. Keep
reusable public instructions parameterized. Do not repopulate this guide with the
operator's environment inventory.

If identifiers or credentials were previously published, report that separately.
A documentation commit changes the current files, not existing Git history,
forks, clones, or caches. Credential revocation and any history cleanup require
their own authorized remediation.
