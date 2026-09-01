# Mira — Infrastructure Reference

**Purpose of this document:** a complete, as-built snapshot of every Azure
resource, identity, role assignment, and Kubernetes object behind the Mira
system, captured on **2026-08-18** ahead of the `dcasati.net` subscription
being retired. Everything below was read live from Azure/`kubectl` at
capture time — this is not a design doc, it's what was actually deployed.

If you're rebuilding this in a new subscription, follow the numbered
sections in order — each one only depends on resources created in earlier
sections. **If an AI agent (Copilot, Microsoft Scout, etc.) is doing the
rebuild, point it at [`REBUILD_PLAYBOOK.md`](REBUILD_PLAYBOOK.md) instead**
— that file is written as step-by-step imperative instructions for an
agent to execute, including what to ask the user for first and where to
pause for portal-only steps; this document is its reference source for
exact resource names and values.

---

## 1. Tenant / subscription at capture time

| | |
|---|---|
| Tenant ID | `de060cb6-6b37-489b-8a6e-c078c6dbeb09` |
| Subscription | `dcasati.net` (`6edaa0d4-86e4-431f-a3e2-d027a34f03c9`) — **being retired** |
| Primary region | East US 2 (`eastus2`) |
| Secondary region (legacy, still live) | West US 3 (`westus3`) |

## 2. Resource inventory by resource group

| Resource group | Region | Contains |
|---|---|---|
| `rg-ITSD-FDSS-POC-eastus2` | eastus2 | **Active** AKS cluster (`aks-mira-eastus2`), Azure Managed Redis (`redis-operator-matrix-eastus2`), GitHub Actions publisher identity (`id-ontology-sync-github`) |
| `rg-ITSD-FDSS-POC` | westus3 | **Legacy** AKS cluster (`aks-ITSD-FDSS-POC-01`), the two workload identities (`id-mira-gateway`, `id-operator-agent-aks`) — note these identities live in the *westus3* RG even though they're now also federated to the eastus2 cluster (see §4) |
| `rg-openai-demo` | eastus2 | Azure OpenAI (`instance-openai-dc`), Azure AI Search (`search-openai-demo`), Microsoft Fabric capacity (`operatorfabricpoc`, SKU F4) |
| `1-rg-chaos-demo` | eastus2 (shared/multi-tenant RG — do not treat as Mira-owned) | Azure AI Foundry account + project (`admin-2434-resource` / project `admin-2434`) — this RG has unrelated resources (ARO, chaos-demo AKS) belonging to other projects; only the Foundry account/project is Mira's |
| `myresourcegroup` | westus3 (shared RG — do not treat as Mira-owned) | Azure Container Registry (`myregistryvkzd`) — only this one resource in the RG is Mira's; the rest (`petclinic*`, `myprometheusvkzd`, etc.) belong to unrelated demos |
| `rg-dcasati-iq-test` | eastus2 | Application Insights (`appi-ailmleybtyfom`) + its Log Analytics workspace (`log-ailmleybtyfom`) — this is where the Foundry hosted agent's `APPLICATIONINSIGHTS_CONNECTION_STRING` points; auto-provisioned alongside an earlier Foundry sandbox, reused here |

---

## 3. AKS clusters

### 3.1 `aks-mira-eastus2` (active — this is where mira-gateway and operator-agent-aks run today)

```
Resource group:     rg-ITSD-FDSS-POC-eastus2
Node resource group: MC_rg-ITSD-FDSS-POC-eastus2_aks-mira-eastus2_eastus2
Location:            eastus2
Kubernetes version:  1.35
SKU:                 Base / Free tier
Node pool:           nodepool1 — 2x Standard_D4s_v4, Linux, System mode, no autoscaling
Network:             Azure CNI, standard LB, AKS-managed VNet (no custom subnet), public API server
OIDC issuer:         enabled — https://eastus2.oic.prod-aks.azure.com/de060cb6-6b37-489b-8a6e-c078c6dbeb09/514a11a4-2731-42b2-93a1-dc834ccd2c89/
Workload Identity:   enabled (securityProfile.workloadIdentity.enabled = true)
Cluster identity:    SystemAssigned (principalId b23df09e-455c-47de-af23-3ce14353d01d)
Kubelet identity:    aks-mira-eastus2-agentpool (clientId 857b066d-59b1-4ca3-ae2e-d289d490deb3) — has AcrPull on myregistryvkzd only
```

Recreate:
```bash
az aks create -g rg-ITSD-FDSS-POC-eastus2 -n aks-mira-eastus2 \
  --location eastus2 \
  --kubernetes-version 1.35 \
  --node-count 2 --node-vm-size Standard_D4s_v4 \
  --enable-oidc-issuer --enable-workload-identity \
  --network-plugin azure --load-balancer-sku standard
```

### 3.2 `aks-ITSD-FDSS-POC-01` (legacy — westus3)

Still running, in `rg-ITSD-FDSS-POC`. This is where `id-mira-gateway` and
`id-operator-agent-aks` were originally created and federated (OIDC issuer
`https://westus3.oic.prod-aks.azure.com/de060cb6-6b37-489b-8a6e-c078c6dbeb09/18ed0ddd-6337-4722-88e4-6a50ab92790c/`).
The same two identities were later **also** federated to the eastus2
cluster (§4.2) so the exact same identity/RBAC could back Mira in either
region without re-provisioning anything. Not required if rebuilding fresh
— only the eastus2 cluster and its federation are needed going forward.

---

## 4. Managed identities

Four user-assigned managed identities in total. None have any secrets/keys
in use anywhere — **every one is Entra Workload Identity (OIDC federation),
zero stored credentials.**

### 4.1 Identity inventory

| Identity | Resource group | Client ID | Used by |
|---|---|---|---|
| `id-mira-gateway` | `rg-ITSD-FDSS-POC` (westus3) | `a2efd0d1-4711-4e23-9289-3b683071d608` | `mira-gateway` pod, namespace `mira` |
| `id-operator-agent-aks` | `rg-ITSD-FDSS-POC` (westus3) | `d12c1fcd-51ef-40bb-899d-a80bb7d8be2e` | `operator-agent-aks` pod, namespace `operator-agent-aks` |
| `id-ontology-sync-github` | `rg-ITSD-FDSS-POC-eastus2` | `f58b85ec-f690-4536-ac38-cfffe2a9ea61` | GitHub Actions workflow only (no k8s pod) |
| `aks-mira-eastus2-agentpool` | auto-created by AKS in the node RG | `857b066d-59b1-4ca3-ae2e-d289d490deb3` | AKS kubelet (image pulls only — `AcrPull`) |
| `operator-persona-agent`'s own agent identity | auto-created by Foundry Agent Service | client/principal `d6e54930-516d-44da-9415-a08e555bb90e` (current, version 11) | The Foundry hosted agent itself — Foundry mints a **new** Entra identity per agent version automatically; this is not something you create or manage directly |

### 4.2 Federated identity credentials (OIDC — no secrets)

```bash
# id-mira-gateway — two federations, one per cluster's OIDC issuer
az identity federated-credential create \
  --name fc-mira-gateway-eastus2 \
  --identity-name id-mira-gateway -g rg-ITSD-FDSS-POC \
  --issuer "https://eastus2.oic.prod-aks.azure.com/de060cb6-6b37-489b-8a6e-c078c6dbeb09/514a11a4-2731-42b2-93a1-dc834ccd2c89/" \
  --subject "system:serviceaccount:mira:mira-gateway" \
  --audience "api://AzureADTokenExchange"

# id-operator-agent-aks — same pattern
az identity federated-credential create \
  --name fc-operator-agent-aks-eastus2 \
  --identity-name id-operator-agent-aks -g rg-ITSD-FDSS-POC \
  --issuer "https://eastus2.oic.prod-aks.azure.com/de060cb6-6b37-489b-8a6e-c078c6dbeb09/514a11a4-2731-42b2-93a1-dc834ccd2c89/" \
  --subject "system:serviceaccount:operator-agent-aks:operator-agent" \
  --audience "api://AzureADTokenExchange"

# id-ontology-sync-github — federated to GitHub's OIDC issuer, not AKS
az identity federated-credential create \
  --name fc-github-actions-main \
  --identity-name id-ontology-sync-github -g rg-ITSD-FDSS-POC-eastus2 \
  --issuer "https://token.actions.githubusercontent.com" \
  --subject "repo:dcasati/operator-matrix-ontology:ref:refs/heads/main" \
  --audience "api://AzureADTokenExchange"
```

> Note: the live subject string on this last credential currently reads
> `repo:dcasati@3240777/operator-matrix-ontology@1334299423:ref:refs/heads/main`
> — the `@<number>` suffixes are almost certainly a GitHub org/repo-ID
> artifact from how the ontology repo was merged into the `mira` monorepo
> later. If recreating in GitHub Actions against the current `mira` repo
> path, use the plain `repo:<owner>/<repo>:ref:refs/heads/main` form shown
> above and re-point the workflow's `AZURE_CLIENT_ID` GitHub Actions
> variable at whichever identity you create.

---

## 5. RBAC — Azure role assignments (ARM control plane)

| Identity | Role | Scope |
|---|---|---|
| `id-mira-gateway` | `Cognitive Services OpenAI User` | `instance-openai-dc` (rg-openai-demo) |
| `id-mira-gateway` | `Foundry User` | Foundry project `admin-2434` (1-rg-chaos-demo) |
| `id-mira-gateway` | `Search Index Data Reader` | `search-openai-demo` (rg-openai-demo) — **legacy, flagged for removal**: leftover from an earlier architecture where mira called Fabric IQ / Search directly; no longer used now that `operator-persona-agent` owns all grounding |
| `id-mira-gateway` | `Search Service Contributor` | `search-openai-demo` (rg-openai-demo) — **same legacy flag as above** |
| `id-operator-agent-aks` | `Foundry User` | Foundry project `admin-2434` (1-rg-chaos-demo) |
| `aks-mira-eastus2-agentpool` (kubelet) | `AcrPull` | `myregistryvkzd` (myresourcegroup) |
| `id-ontology-sync-github` | *(none)* | This identity has **zero** ARM role assignments — it only needs the Redis data-plane access policy below, confirmed by testing (the GitHub Actions workflow explicitly passes `allow-no-subscriptions: true` to `azure/login` because it has no subscription-scoped role at all) |

Recreate the two active roles:
```bash
az role assignment create --assignee <mira-gateway-principal-id> \
  --role "Cognitive Services OpenAI User" \
  --scope /subscriptions/<sub>/resourceGroups/rg-openai-demo/providers/Microsoft.CognitiveServices/accounts/instance-openai-dc

az role assignment create --assignee <mira-gateway-principal-id> \
  --role "Foundry User" \
  --scope /subscriptions/<sub>/resourceGroups/1-rg-chaos-demo/providers/Microsoft.CognitiveServices/accounts/admin-2434-resource/projects/admin-2434

az role assignment create --assignee <operator-agent-aks-principal-id> \
  --role "Foundry User" \
  --scope /subscriptions/<sub>/resourceGroups/1-rg-chaos-demo/providers/Microsoft.CognitiveServices/accounts/admin-2434-resource/projects/admin-2434
```
Do **not** recreate the two legacy Search roles on `id-mira-gateway` in a
fresh deployment — they're not needed by any current code path.

## 6. Data-plane access (not ARM RBAC — separate from §5)

### 6.1 Azure Managed Redis access policy assignments
`redis-operator-matrix-eastus2` (rg-ITSD-FDSS-POC-eastus2), database `default`, port 10000, Entra-ID-only auth (`accessKeysAuthentication: Disabled`):

| Access policy assignment | Principal (object ID) | Identity |
|---|---|---|
| `operatorpersonaagent` | `d6e54930-516d-44da-9415-a08e555bb90e` | Foundry hosted agent's own auto-issued identity (**changes if the agent is redeployed from scratch** — re-grant after any full agent recreation) |
| `operatoragentaks` | `7d1f9eae-7ff1-41fe-aec1-84fed6c11f09` | `id-operator-agent-aks` |
| `githubactionspublisher` | `97acf4c5-dae0-468b-810a-b640f9072a7c` | `id-ontology-sync-github` |

```bash
az redisenterprise database access-policy-assignment create \
  --cluster-name redis-operator-matrix-eastus2 -g rg-ITSD-FDSS-POC-eastus2 \
  --database-name default --access-policy-assignment-name operatoragentaks \
  --access-policy-name default --user-object-id <operator-agent-aks-principal-id>
# repeat for operatorpersonaagent and githubactionspublisher with their principal IDs
```

### 6.2 Microsoft Fabric workspace
Both agents need **Member** role on the Fabric workspace containing the
`operator_matrix_ontology` Ontology item (workspace ID
`71f18f4c-2832-4dbc-9fdf-178b0954cc38`), granted to each agent's own
identity (Foundry hosted agent identity, and `id-operator-agent-aks`). This
is set in the Fabric portal (Workspace → Manage access), not via `az` —
Fabric workspace role assignment isn't an ARM/RBAC operation.

The workspace is backed by Fabric capacity `operatorfabricpoc` (F4 SKU, rg-openai-demo).

---

## 7. Kubernetes resources

### 7.1 Namespaces
- `mira` — mira-gateway
- `operator-agent-aks` — the AKS self-hosted agent variant

### 7.2 `mira-gateway` (namespace `mira`)

| | |
|---|---|
| ServiceAccount | `mira-gateway`, annotated `azure.workload.identity/client-id: a2efd0d1-4711-4e23-9289-3b683071d608` |
| Deployment | pod template label `azure.workload.identity/use: "true"`, `app.kubernetes.io/name: mira`, `app.kubernetes.io/component: gateway` |
| Image | `myregistryvkzd.azurecr.io/mira@sha256:...` — see `services/mira-gateway/Dockerfile`; build via `az acr build --registry myregistryvkzd --image mira:latest .` |
| Resources | requests 500m CPU / 512Mi, limits 2 CPU / 2Gi |
| Probes | `/healthz` (liveness), `/readyz` (readiness), both on container port 8080 |
| Pod security | `runAsNonRoot`, seccomp `RuntimeDefault`, container `allowPrivilegeEscalation: false`, capabilities dropped `ALL`, `readOnlyRootFilesystem: true` |
| Secret `mira-zello` | keys: `ZELLO_AUTH_TOKEN`, `ZELLO_CHANNEL`, `ZELLO_PASSWORD`, `ZELLO_USERNAME` — **values are external Zello Work credentials, not Azure** (see §10) |
| ConfigMap `mira-config` | see `k8s/eastus2/configmap.yaml` in the repo for the current full non-secret config (Zello endpoint, Azure OpenAI Realtime deployment name, Foundry IQ agent name/project endpoint, telemetry settings, `MIRA_CONVERSATION_TIMEOUT`, etc.) |

Manifests live at `services/mira-gateway/k8s/eastus2/` in the `mira` repo —
apply with `kubectl apply -f services/mira-gateway/k8s/eastus2/` after
creating the namespace, ServiceAccount annotation, and secret.

### 7.3 `operator-agent-aks` (namespace `operator-agent-aks`)

| | |
|---|---|
| ServiceAccount | `operator-agent`, annotated `azure.workload.identity/client-id: d12c1fcd-51ef-40bb-899d-a80bb7d8be2e` |
| Deployment | label `app.kubernetes.io/name: operator-agent-aks`, `azure.workload.identity/use: "true"` |
| Image | `myregistryvkzd.azurecr.io/operator-agent-aks@sha256:...` — see `services/operator-agent/src/operator-persona-agent-aks/Dockerfile` |
| Service | ClusterIP, port 8088 — **not exposed publicly**, no auth of its own; only reachable in-cluster (this is how `mira-gateway` calls it directly when `FOUNDRY_IQ_DIRECT_ENDPOINT` is set instead of the Foundry project endpoint) |
| Resources | requests 250m CPU / 256Mi, limits 1 CPU / 1Gi |
| ConfigMap `operator-agent-config` | `AZURE_AI_MODEL_DEPLOYMENT_NAME=gpt-5-mini`, `FABRIC_WORKSPACE_ID=71f18f4c-2832-4dbc-9fdf-178b0954cc38`, `FOUNDRY_IQ_SEARCH_ENDPOINT=https://search-openai-demo.search.windows.net`, `FOUNDRY_PROJECT_ENDPOINT=https://admin-2434-resource.services.ai.azure.com/api/projects/admin-2434`, `PORT=8088`, `REDIS_HOST=redis-operator-matrix-eastus2.eastus2.redis.azure.net`, `REDIS_PORT=10000` |
| Secret `operator-agent-secrets` | keys: `APPLICATIONINSIGHTS_CONNECTION_STRING`, `FOUNDRY_IQ_SEARCH_API_KEY` |

Manifests: `services/operator-agent/src/operator-persona-agent-aks/k8s/`.

---

## 8. Azure OpenAI — `instance-openai-dc` (rg-openai-demo, eastus2)

Endpoint: `https://instance-openai-dc.openai.azure.com/`

| Deployment name | Model | Version | SKU | Capacity |
|---|---|---|---|---|
| `mira-gpt-realtime-mini` | gpt-realtime-mini | 2025-12-15 | GlobalStandard | 10 — **the one Mira actually uses**, for the Realtime voice conversation |
| gpt-4o | gpt-4o | 2024-11-20 | GlobalStandard | 100 |
| gpt-35-turbo | gpt-4.1-mini | 2025-04-14 | Standard | 100 |
| gpt-5.1-chat | gpt-chat-latest | 2026-05-05 | GlobalStandard | 1000 |
| gpt-5-mini | gpt-5-mini | 2025-08-07 | GlobalStandard | 1000 — used by both operator-agent variants for the actual grounding/reasoning model |

Recreate the one Mira depends on:
```bash
az cognitiveservices account create -n instance-openai-dc -g rg-openai-demo \
  --kind OpenAI --sku S0 --location eastus2

az cognitiveservices account deployment create \
  -n instance-openai-dc -g rg-openai-demo \
  --deployment-name mira-gpt-realtime-mini \
  --model-name gpt-realtime-mini --model-version 2025-12-15 \
  --model-format OpenAI --sku-name GlobalStandard --sku-capacity 10
```

## 9. Azure AI Search — `search-openai-demo` (rg-openai-demo, eastus2)

Standard SKU, default hosting mode. Backs the `kb-zavatrix` knowledge base
(radio manuals/PDFs/field notes) the agent queries via Azure AI Search MCP,
api-key auth only. `id-mira-gateway`'s two role assignments here
(`Search Index Data Reader`, `Search Service Contributor`) are legacy and
unused (§5) — the agent itself authenticates with a plain API key stored
in its own secret (`FOUNDRY_IQ_SEARCH_API_KEY`), not RBAC.

## 10. Zello (external SaaS — not an Azure resource)

Mira connects to Zello's **Channel API** (`wss://zello.io/ws`) using a
named-account logon: `ZELLO_USERNAME` / `ZELLO_PASSWORD` /
`ZELLO_CHANNEL` / `ZELLO_AUTH_TOKEN`. Rebuilding this requires:
1. A Zello Work account with Channel API access and the target channel
   (`Caldova` as of 2026-09-01 — previously `Zavatrix`, retired; do not
   confuse this with `kb-zavatrix`, the unrelated Azure AI Search knowledge
   base name in §11, which was never a channel name and hasn't changed).
2. A JWT auth token signed with the account's Zello developer-portal Issuer/Private Key (RS256, `iss`+`exp` payload) for production; the 30-day sample dev token for local testing only.
3. None of this is stored in Azure Key Vault currently — it's a plain Kubernetes Secret (`mira-zello`). See `services/mira-gateway/README.md` for the exact logon frame shape.

---

## 11. Azure AI Foundry — hosted agent (`operator-persona-agent`)

```
Account:  admin-2434-resource   (1-rg-chaos-demo, eastus2, kind AIServices, SKU S0)
Project:  admin-2434
Endpoint: https://admin-2434-resource.services.ai.azure.com/api/projects/admin-2434
Agent:    operator-persona-agent (current version: 11, 100% traffic)
Sandbox:  0.5 vCPU / 1 GiB memory
Runtime:  python_3_13, entry point `python main.py`, dependency_resolution=remote_build
Protocol: responses (v2.0.0)
```

Agent environment variables (set at version-creation time, immutable per
version):
```
FOUNDRY_PROJECT_ENDPOINT       = https://admin-2434-resource.services.ai.azure.com/api/projects/admin-2434
AZURE_AI_MODEL_DEPLOYMENT_NAME = gpt-5-mini
FOUNDRY_IQ_SEARCH_ENDPOINT     = https://search-openai-demo.search.windows.net
FOUNDRY_IQ_SEARCH_API_KEY      = <secret>
FABRIC_WORKSPACE_ID            = 71f18f4c-2832-4dbc-9fdf-178b0954cc38
REDIS_HOST                     = redis-operator-matrix-eastus2.eastus2.redis.azure.net
REDIS_PORT                     = 10000
APPLICATIONINSIGHTS_CONNECTION_STRING = <from appi-ailmleybtyfom, rg-dcasati-iq-test>
```

Recreate/redeploy with the repo's own script (handles zip packaging,
version creation, polling for `active`, and 100%-traffic routing):
```bash
cd services/operator-agent
# .env needs: FOUNDRY_PROJECT_ENDPOINT, FOUNDRY_MODEL_NAME, FOUNDRY_SAMPLE_PATH
#             (= src/operator-persona-agent), FOUNDRY_IQ_SEARCH_ENDPOINT,
#             FOUNDRY_IQ_SEARCH_API_KEY, FABRIC_WORKSPACE_ID,
#             REDIS_HOST, REDIS_PORT, APPLICATIONINSIGHTS_CONNECTION_STRING
python deploy_hosted_agent.py
```

**Important — every redeploy issues a brand-new agent identity**
(`instance_identity.principal_id` changes on each version, e.g. current
`d6e54930-516d-44da-9415-a08e555bb90e`). After any full redeploy, you must
re-grant that new identity:
- `Foundry User` is implicit (it's the project's own agent) — no action needed
- Redis access-policy-assignment for the new principal ID (§6.1)
- Fabric workspace Member role for the new principal ID (§6.2)

**Known operational finding (2026-08-17/18, live-tested):** the hosted
agent has **no session reuse by default** — every call without a chained
`previous_response_id` provisions a brand-new per-session VM-isolated
sandbox from scratch (confirmed via the agent's own session directory:
`GET .../agents/operator-persona-agent/endpoint/sessions` showed 39+
distinct sessions, one per call ever made). A cold call measured
35-56s end-to-end; the same call warm (with `previous_response_id`
chained) measured ~11.3s. `mira-gateway`'s client now chains this
automatically per Zello speaker (see `internal/foundryiq/client.go`), but
even warm, this path still has meaningfully more latency and less
reliability (`session_not_ready` 424s observed under back-to-back calls)
than the AKS self-hosted variant, which has no per-call sandbox at all.

---

## 12. Azure Managed Redis — `redis-operator-matrix-eastus2`

```
Resource group: rg-ITSD-FDSS-POC-eastus2, eastus2
Kind:           v2 (Redis Enterprise)
Redis version:  7.4
Database:       default, port 10000, clustering OSSCluster
Auth:           accessKeysAuthentication Disabled — Entra ID only
TLS:            min 1.2, clientProtocol Encrypted
High availability: Enabled, redundancy ZR (zone-redundant)
```

Purpose: caches the `operator_matrix_ontology` Fabric Ontology's parsed
schema (entities, table bindings, relationships) at key
`operator_matrix_ontology:schema:v1`, published by the GitHub Actions
pipeline (§13). Both agent variants try Redis first at startup and fall
back to live Fabric `getDefinition` discovery if Redis is empty or
unreachable — it's a cache, never a hard dependency.

```bash
az redisenterprise create -n redis-operator-matrix-eastus2 -g rg-ITSD-FDSS-POC-eastus2 \
  --location eastus2 --sku Balanced_B0 \
  --minimum-tls-version 1.2

az redisenterprise database create \
  --cluster-name redis-operator-matrix-eastus2 -g rg-ITSD-FDSS-POC-eastus2 \
  --database-name default --port 10000 --clustering-policy OSSCluster \
  --client-protocol Encrypted --eviction-policy VolatileLRU \
  --access-keys-authentication Disabled
```

## 13. Ontology governance pipeline

Full detail already documented in-repo — this section just anchors it in
the infra picture:
- `mira/ontology-governance/README.md` — pipeline overview
- `mira/ontology-governance/fabric-workspace/README.md` — Fabric Git integration target folder
- `mira/ontology-governance/.github/workflows/publish-ontology.yml` — publish-on-push-to-main workflow, OIDC login via `id-ontology-sync-github`, no stored secret
- `mira/ontology-governance/.github/workflows/validate-ontology-pr.yml` — PR-time dry-run validation, no Azure credentials needed

GitHub Actions repo variables required (Settings → Secrets and variables →
Actions → Variables, on whichever repo carries `ontology-governance/`):
```
AZURE_CLIENT_ID          = <id-ontology-sync-github's client ID>
AZURE_TENANT_ID          = de060cb6-6b37-489b-8a6e-c078c6dbeb09
REDIS_HOST               = redis-operator-matrix-eastus2.eastus2.redis.azure.net
REDIS_PORT               = 10000
AZURE_IDENTITY_OBJECT_ID = <id-ontology-sync-github's principal/object ID>
```

Fabric side: connect the `operator_matrix_ontology` workspace's Git
integration (Workspace settings → Git integration, in the Fabric portal)
to the `ontology-governance/fabric-workspace/` folder in whichever repo
hosts it.

---

## 14. Container Registry — `myregistryvkzd` (myresourcegroup, westus3)

Standard SKU. Holds both `mira` (gateway) and `operator-agent-aks` images.
AKS kubelet identity has `AcrPull` scoped to this registry (§5).

```bash
az acr create -n myregistryvkzd -g myresourcegroup --sku Standard --location westus3
az aks update -n aks-mira-eastus2 -g rg-ITSD-FDSS-POC-eastus2 --attach-acr myregistryvkzd
```

Build/push:
```bash
az acr build --registry myregistryvkzd --image mira:latest -f services/mira-gateway/Dockerfile services/mira-gateway
az acr build --registry myregistryvkzd --image operator-agent-aks:latest -f services/operator-agent/src/operator-persona-agent-aks/Dockerfile services/operator-agent/src/operator-persona-agent-aks
```

## 15. Application Insights — `appi-ailmleybtyfom` (rg-dcasati-iq-test, eastus2)

Backs the Foundry hosted agent's OTEL traces (auto-instrumented by the
Hosted Agent protocol libraries — every `invoke_agent` span, network
egress decisions, etc. land here, queryable via
`az monitor app-insights query`). Log Analytics workspace:
`log-ailmleybtyfom` in the same RG.

```bash
az monitor log-analytics workspace create -n log-ailmleybtyfom -g rg-dcasati-iq-test --location eastus2
az monitor app-insights component create -n appi-ailmleybtyfom -g rg-dcasati-iq-test \
  --location eastus2 --workspace log-ailmleybtyfom --application-type web
```

---

## 16. GitHub repositories

| Repo | Role |
|---|---|
| `github.com/microsoftgbb/mira` | Upstream/canonical monorepo — merges `mira-gateway` + `operator-agent` + `ontology-governance` |
| `github.com/dcasati/mira` | Personal fork, kept in sync (`origin` remote in the local checkout); this is where Fabric's Git integration should point if reconnected |
| `github.com/dcasati/mira-operator-agent` | Pre-merge standalone repo for the operator agent — superseded by the monorepo but still exists |

The monorepo was formed via `git subtree` merges of three previously
separate repos (`dcasati/mira`, `dcasati/mira-operator-agent`,
`dcasati/operator-matrix-ontology`), preserving each one's full commit
history under its new path — see the root `README.md`'s "History note".

---

## 17. Rebuild order (fresh subscription, from zero)

1. **ACR** (§14) — needed before anything can push images.
2. **AKS cluster** (§3.1), with `--enable-oidc-issuer --enable-workload-identity`, then `az aks update --attach-acr`.
3. **Managed identities** (§4.1) — create all three (`id-mira-gateway`, `id-operator-agent-aks`, `id-ontology-sync-github`), then federate each to its cluster/GitHub OIDC issuer (§4.2) — you'll only know the cluster's real OIDC issuer URL *after* step 2, so this must come after.
4. **Azure OpenAI** (§8) + **Azure AI Search** (§9) + **Microsoft Fabric capacity/workspace** (§6.2) — provision the data/knowledge layer.
5. **Azure Managed Redis** (§12).
6. **Role assignments** (§5) + **Redis access policies** (§6.1) + **Fabric workspace Member role** (§6.2) for `id-mira-gateway` and `id-operator-agent-aks`.
7. **Azure AI Foundry account/project**, then deploy `operator-persona-agent` (§11) — grab its auto-issued agent identity and grant it the same Redis + Fabric access as step 6.
8. **App Insights** (§15) if you want hosted-agent tracing.
9. **Kubernetes**: namespaces, ServiceAccounts (with the workload-identity client-id annotations from step 3), Secrets (Zello creds §10, Search API key, App Insights connection string), ConfigMaps, Deployments (§7) — build/push images first (§14).
10. **Ontology governance repo + GitHub Actions variables** (§13) if you want the governed Redis-publish pipeline instead of manually seeding Redis.
11. **Zello Work account + channel** (§10) — the only genuinely external, non-Azure dependency.

---

## 18. Known follow-ups (not yet acted on)

- Remove the two legacy Search roles on `id-mira-gateway` (§5) — confirmed unused.
- The Foundry hosted-agent path (`operator-persona-agent`) is measurably
  slower and less reliable under back-to-back calls than the AKS
  self-hosted variant (§11) — worth revisiting sandbox size (currently the
  smallest tier, 0.5 vCPU/1GiB) if continuing to invest in that path.
- The GitHub Actions OIDC federation subject on `id-ontology-sync-github`
  has an unexplained `@<number>` suffix (§4.2) — cosmetic today since the
  workflow still authenticates successfully, but worth cleaning up if the
  identity is recreated from scratch.
