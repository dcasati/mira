# Mira — Agent Rebuild Playbook

**Read this file first if a user asks you (Copilot / Microsoft Scout / any
AI agent) to "rebuild Mira", "set up Mira in a new subscription", "recreate
the Mira infrastructure", or similar.** This is a self-contained, imperative
runbook written for an AI agent to execute, not just a human reference.
Pair it with [`INFRASTRUCTURE.md`](INFRASTRUCTURE.md) in this same folder,
which has the full as-built detail (exact resource names, role assignments,
config values) this playbook's commands are drawn from — consult it
whenever this playbook says "see INFRASTRUCTURE.md §N".

This playbook assumes you have `az` (logged in), `kubectl`, and `git`
available, and shell/tool access equivalent to what produced
INFRASTRUCTURE.md (Azure CLI, kubectl, bash).

---

## Step 0 — Gather required inputs before doing anything

Do not guess these. Ask the user directly (or check the invoking context)
for:

1. **Target subscription ID and tenant** — `az account show` to confirm
   you're pointed at the right one before creating anything.
2. **Target region** — INFRASTRUCTURE.md's original deployment used
   `eastus2` for everything except a legacy `westus3` cluster that is
   **not required** for a fresh build. Default to a single region
   (`eastus2` or whatever the user prefers) unless told otherwise — do not
   recreate the legacy westus3 split.
3. **Resource naming** — the original names (`aks-mira-eastus2`,
   `myregistryvkzd`, `redis-operator-matrix-eastus2`, etc.) are specific to
   the old subscription. Ask the user whether to reuse these names or pick
   new ones (useful if some are globally-unique names like the ACR and
   might already be taken). Substitute their answers into every command
   below.
4. **GitHub repo** for the ontology-governance pipeline — ask which
   GitHub org/repo will host `ontology-governance/` (it may be a fresh
   repo, or the existing `mira` monorepo path). You need the exact
   `owner/repo` string for the OIDC federated-credential subject in Step 5.
5. **Zello Work account** — this is the one piece with no Azure equivalent
   (§10 in INFRASTRUCTURE.md). Ask the user whether they have an existing
   Zello Work account + channel to reuse, or need to create one. You
   cannot provision this yourself — pause and wait for them to supply
   `ZELLO_USERNAME`, `ZELLO_PASSWORD`, `ZELLO_CHANNEL`, and either a fresh
   dev token or a JWT signed with their Issuer/Private Key.
6. **Fabric capacity/workspace** — ask whether the user has an existing
   Fabric workspace with an `operator_matrix_ontology` Ontology item to
   reuse, or needs one created fresh. Fabric workspace creation, the
   Ontology item itself, and workspace role grants are **Fabric-portal-only
   operations with no `az` CLI equivalent** — you cannot do these steps for
   the user. Pause at Step 4 below and have them confirm this is done
   before you continue past it.

Confirm your understanding of all six back to the user before proceeding —
a wrong region or name choice early on cascades into every later step.

---

## Step 1 — Container Registry (no dependencies)

```bash
az acr create -n <ACR_NAME> -g <ACR_RG> --sku Standard --location <REGION>
```
**Verify:** `az acr show -n <ACR_NAME> --query loginServer -o tsv` returns a
value.

## Step 2 — AKS cluster

```bash
az aks create -g <AKS_RG> -n <AKS_NAME> \
  --location <REGION> \
  --kubernetes-version 1.35 \
  --node-count 2 --node-vm-size Standard_D4s_v4 \
  --enable-oidc-issuer --enable-workload-identity \
  --network-plugin azure --load-balancer-sku standard

az aks update -n <AKS_NAME> -g <AKS_RG> --attach-acr <ACR_NAME>

az aks get-credentials -n <AKS_NAME> -g <AKS_RG> --overwrite-existing
```
**Verify:** `kubectl get nodes` shows 2 `Ready` nodes.

Capture the cluster's OIDC issuer URL now — you need it in Step 3 and it
does not exist before this step completes:
```bash
OIDC_ISSUER=$(az aks show -g <AKS_RG> -n <AKS_NAME> --query oidcIssuerProfile.issuerUrl -o tsv)
echo "$OIDC_ISSUER"
```

## Step 3 — Managed identities + federated credentials

Create all three identities (see INFRASTRUCTURE.md §4.1 for what each is
for), then federate each — **this step must come after Step 2**, since it
needs `$OIDC_ISSUER`:

```bash
az identity create -n id-mira-gateway -g <IDENTITY_RG> --location <REGION>
az identity create -n id-operator-agent-aks -g <IDENTITY_RG> --location <REGION>
az identity create -n id-ontology-sync-github -g <IDENTITY_RG> --location <REGION>

az identity federated-credential create \
  --name fc-mira-gateway --identity-name id-mira-gateway -g <IDENTITY_RG> \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:mira:mira-gateway" \
  --audience "api://AzureADTokenExchange"

az identity federated-credential create \
  --name fc-operator-agent-aks --identity-name id-operator-agent-aks -g <IDENTITY_RG> \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:operator-agent-aks:operator-agent" \
  --audience "api://AzureADTokenExchange"

az identity federated-credential create \
  --name fc-github-actions --identity-name id-ontology-sync-github -g <IDENTITY_RG> \
  --issuer "https://token.actions.githubusercontent.com" \
  --subject "repo:<GITHUB_OWNER>/<GITHUB_REPO>:ref:refs/heads/main" \
  --audience "api://AzureADTokenExchange"
```
**Verify:** `az identity federated-credential list --identity-name <name> -g <IDENTITY_RG>` returns one entry per identity, each with the expected `subject`.

## Step 4 — Data/knowledge layer

Create in this order — later resources reference earlier ones:

```bash
# Azure OpenAI + the Realtime deployment mira actually uses
az cognitiveservices account create -n <AOAI_NAME> -g <AOAI_RG> --kind OpenAI --sku S0 --location <REGION>
az cognitiveservices account deployment create -n <AOAI_NAME> -g <AOAI_RG> \
  --deployment-name mira-gpt-realtime-mini \
  --model-name gpt-realtime-mini --model-version 2025-12-15 \
  --model-format OpenAI --sku-name GlobalStandard --sku-capacity 10
# Also create a gpt-5-mini (or current equivalent) deployment for the
# operator agent's own reasoning model -- see INFRASTRUCTURE.md §8.

# Azure AI Search (backs the kb-zavatrix radio-manuals knowledge base)
az search service create -n <SEARCH_NAME> -g <SEARCH_RG> --sku standard --location <REGION>
```

**PAUSE HERE.** Fabric workspace + Ontology item creation, and Fabric
Git integration setup, are Fabric-portal-only — there is no `az` command
for them. Ask the user to confirm:
- [ ] A Fabric workspace exists with capacity assigned (F-SKU or trial)
- [ ] It contains (or will contain) an Ontology item named
      `operator_matrix_ontology`, with EntityTypes bound to Lakehouse
      tables (see INFRASTRUCTURE.md §11's `operator_matrix_data.py`
      reference for what the agent expects to discover)
- [ ] They have the workspace ID handy (`FABRIC_WORKSPACE_ID` — a GUID,
      visible in the workspace's URL or Settings)

Do not proceed past this point until the user confirms the workspace
exists and gives you its workspace ID.

## Step 5 — Azure Managed Redis

```bash
az redisenterprise create -n <REDIS_NAME> -g <REDIS_RG> \
  --location <REGION> --sku Balanced_B0 --minimum-tls-version 1.2

az redisenterprise database create \
  --cluster-name <REDIS_NAME> -g <REDIS_RG> \
  --database-name default --port 10000 --clustering-policy OSSCluster \
  --client-protocol Encrypted --eviction-policy VolatileLRU \
  --access-keys-authentication Disabled
```
**Verify:** `az redisenterprise database show --cluster-name <REDIS_NAME> -g <REDIS_RG> --database-name default --query provisioningState -o tsv` returns `Succeeded`.

## Step 6 — Grant RBAC + data-plane access for the two AKS identities

```bash
MG_PID=$(az identity show -n id-mira-gateway -g <IDENTITY_RG> --query principalId -o tsv)
OA_PID=$(az identity show -n id-operator-agent-aks -g <IDENTITY_RG> --query principalId -o tsv)

az role assignment create --assignee "$MG_PID" --role "Cognitive Services OpenAI User" \
  --scope $(az cognitiveservices account show -n <AOAI_NAME> -g <AOAI_RG> --query id -o tsv)

az redisenterprise database access-policy-assignment create \
  --cluster-name <REDIS_NAME> -g <REDIS_RG> --database-name default \
  --access-policy-assignment-name operatoragentaks \
  --access-policy-name default --user-object-id "$OA_PID"
```
Do **not** grant `id-mira-gateway` any Search roles — INFRASTRUCTURE.md
§5 flags those as legacy/unused in the current architecture.

**PAUSE HERE.** In the Fabric portal, grant **Member** role on the
workspace to both `$MG_PID` and `$OA_PID` (and, after Step 8, the Foundry
hosted agent's own auto-issued identity) — this is Fabric-portal-only, no
CLI equivalent. Confirm with the user this is done before continuing.

## Step 7 — Kubernetes resources

Build and push images first (this uses `az acr build`, so it always builds
the full production image — see `services/mira-gateway/README.md`'s "Build
and test locally" section instead if you need a native `bin/mira` binary
for local dev/testing outside a container):
```bash
az acr build --registry <ACR_NAME> --image mira:latest \
  -f services/mira-gateway/Dockerfile services/mira-gateway
az acr build --registry <ACR_NAME> --image operator-agent-aks:latest \
  -f services/operator-agent/src/operator-persona-agent-aks/Dockerfile \
  services/operator-agent/src/operator-persona-agent-aks
```

Create namespaces, ServiceAccounts (annotated with each identity's client
ID — **not** principal/object ID), Secrets, ConfigMaps, then Deployments.
Use `services/mira-gateway/k8s/` and
`services/operator-agent/src/operator-persona-agent-aks/k8s/` as the
manifest source, editing image references, ConfigMap values (region,
resource names/endpoints from the steps above), and Secret values (Zello
creds from Step 0, Search API key, App Insights connection string from
Step 9) to match this deployment.

```bash
kubectl create namespace mira
kubectl create namespace operator-agent-aks

kubectl create serviceaccount mira-gateway -n mira
kubectl annotate serviceaccount mira-gateway -n mira \
  azure.workload.identity/client-id=$(az identity show -n id-mira-gateway -g <IDENTITY_RG> --query clientId -o tsv)

kubectl create serviceaccount operator-agent -n operator-agent-aks
kubectl annotate serviceaccount operator-agent -n operator-agent-aks \
  azure.workload.identity/client-id=$(az identity show -n id-operator-agent-aks -g <IDENTITY_RG> --query clientId -o tsv)

kubectl create secret generic mira-zello -n mira \
  --from-literal=ZELLO_USERNAME="<from Step 0>" \
  --from-literal=ZELLO_PASSWORD="<from Step 0>" \
  --from-literal=ZELLO_CHANNEL="<from Step 0>" \
  --from-literal=ZELLO_AUTH_TOKEN="<from Step 0>"

kubectl create secret generic operator-agent-secrets -n operator-agent-aks \
  --from-literal=FOUNDRY_IQ_SEARCH_API_KEY="$(az search admin-key show -g <SEARCH_RG> --service-name <SEARCH_NAME> --query primaryKey -o tsv)" \
  --from-literal=APPLICATIONINSIGHTS_CONNECTION_STRING="<from Step 9>"

kubectl apply -f services/mira-gateway/k8s/          # edited configmap.yaml + deployment.yaml
kubectl apply -f services/operator-agent/src/operator-persona-agent-aks/k8s/
```
**Verify:** `kubectl get pods -n mira` and `kubectl get pods -n
operator-agent-aks` both show `1/1 Running`. Tail logs on each
(`kubectl logs -n mira <pod> -f`) and confirm `foundry_iq.enabled` (or
`FOUNDRY_IQ_DIRECT_ENDPOINT` in-cluster mode) and `zello.channel_joined`
appear with no errors.

## Step 8 — Azure AI Foundry hosted agent

```bash
az cognitiveservices account create -n <FOUNDRY_ACCOUNT> -g <FOUNDRY_RG> \
  --kind AIServices --sku S0 --location <REGION>
# Create a Foundry project under that account via the Foundry portal or
# `az cognitiveservices account project create` (naming/CLI surface for
# projects changes across API versions -- check current `az` docs).

cd services/operator-agent
# populate .env: FOUNDRY_PROJECT_ENDPOINT, FOUNDRY_MODEL_NAME,
# FOUNDRY_SAMPLE_PATH=src/operator-persona-agent, FOUNDRY_IQ_SEARCH_ENDPOINT,
# FOUNDRY_IQ_SEARCH_API_KEY, FABRIC_WORKSPACE_ID, REDIS_HOST, REDIS_PORT,
# APPLICATIONINSIGHTS_CONNECTION_STRING
python deploy_hosted_agent.py
```
**Verify:** the script prints `Agent endpoint configured for version N` and
a live test response. Note the printed agent identity's principal ID —
grant it Redis access-policy-assignment (Step 6's pattern) and Fabric
workspace Member role (Step 6's Fabric pause) before considering this
step done. **Every future redeploy of this agent issues a new identity —
repeat this grant step after any full redeploy.**

## Step 9 — Application Insights (optional but recommended)

```bash
az monitor log-analytics workspace create -n <LAW_NAME> -g <LAW_RG> --location <REGION>
az monitor app-insights component create -n <APPI_NAME> -g <APPI_RG> \
  --location <REGION> --workspace <LAW_NAME> --application-type web
az monitor app-insights component show --app <APPI_NAME> -g <APPI_RG> \
  --query connectionString -o tsv
```
Feed the resulting connection string into Step 7's
`operator-agent-secrets` and Step 8's `.env` (do this before Steps 7/8 if
following in strict order, or patch both secrets afterward).

## Step 10 — Ontology governance pipeline (optional; can seed Redis manually instead)

If you want the governed GitHub Actions publish flow instead of manually
seeding Redis:
1. Push `ontology-governance/` to the GitHub repo confirmed in Step 0.
2. In that repo's Settings → Secrets and variables → Actions → Variables, set:
   ```
   AZURE_CLIENT_ID          = <id-ontology-sync-github's clientId>
   AZURE_TENANT_ID          = <tenant ID>
   REDIS_HOST               = <redis hostname from Step 5>
   REDIS_PORT               = 10000
   AZURE_IDENTITY_OBJECT_ID = <id-ontology-sync-github's principalId>
   ```
3. **PAUSE** — in the Fabric portal, connect the workspace's Git
   integration (Workspace settings → Git integration) to this repo's
   `ontology-governance/fabric-workspace/` folder. Confirm with the user.
4. Grant `id-ontology-sync-github`'s principal ID a Redis
   access-policy-assignment (Step 6's pattern, name it
   `githubactionspublisher`).
5. Trigger a commit from Fabric (or `workflow_dispatch` the GitHub Action
   manually) and confirm the workflow run succeeds.

If skipping this, seed Redis directly instead: run
`ontology-governance/scripts/transform_ontology.py` and
`publish_to_redis.py` locally with an interactively-acquired Entra token,
or have the agent fall back to live Fabric discovery (works automatically,
just slower — see INFRASTRUCTURE.md §11).

---

## Final smoke test

1. `kubectl logs -n mira <mira-gateway-pod> --since=5m` — confirm
   `zello.channel_joined` with no reconnect loop.
2. `kubectl logs -n operator-agent-aks <pod> --since=5m` — confirm it
   started cleanly (Redis schema load succeeded or fell back to Fabric
   discovery without erroring).
3. Send a live test question over the configured Zello channel and confirm
   a spoken response comes back.
4. If using the Foundry hosted-agent path instead of/alongside the
   in-cluster one, `curl` its Responses endpoint directly first (see
   INFRASTRUCTURE.md §11) before routing real traffic to it — confirm
   HTTP 200 and a sensible answer.

Report back to the user with a summary of what was created, any step you
had to pause on waiting for their input, and any deviations from this
playbook (different names/regions/SKUs) so they can update
INFRASTRUCTURE.md accordingly for the next person.
