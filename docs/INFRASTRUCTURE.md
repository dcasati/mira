# MIRA infrastructure guide

Use this guide and the [agent rebuild playbook](REBUILD_PLAYBOOK.md) to
recreate MIRA in an environment selected by its operator. This is a deployment
contract, not an inventory of the repository owner's Azure resources.
Subscription IDs, tenant IDs, resource names, endpoints, identity IDs, channel
names, and workspace IDs must come from the target environment.

Reading these instructions does not authorize provisioning, changing access,
connecting to a radio channel, or sending messages. Obtain the approvals in the
playbook before those actions.

## Deployment inputs

Collect these inputs in a private deployment record, not in this public guide.
Use symbolic variable names in shared plans and examples.

| Input | How to choose or obtain it |
|---|---|
| `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID` | Operator-approved target. Verify the signed-in account belongs to that tenant and subscription. |
| `LOCATION` | Region supporting the selected services, model versions, data residency, and quota. Multiple regions require an explicit design decision. |
| Resource groups and naming prefix | New names following the target organization's policy. Identify any approved reuse and ownership boundaries. |
| `ACR_NAME`, `AKS_NAME`, `AKS_RESOURCE_GROUP` | Names for the registry and cluster, or explicit authorization to use existing resources. |
| AKS version, node sizes, count, networking | Supported versions and available capacity, with approved cost, ingress, egress, DNS, and isolation requirements. |
| Model resource, deployment names, versions, capacity | Compatible Realtime and, if needed, reasoning models. Confirm availability and quota at deployment time. |
| Agent implementation and grounding routes | Choose a source directory and its actual dependencies. Do not assume every implementation supports every integration. |
| Kubernetes namespaces and service accounts | Operator-selected names. Keep federation subjects, Services, configuration, and workload references consistent. |
| Zello account, endpoint, and channel | Dedicated bot account with approved Channel API access. Confirm the account product and supported authentication flow. |
| Secret delivery mechanism | Organization-approved secret store and injection method. Credentials are supplied out of band, not in chat or tracked files. |
| Optional service inputs | Fabric workspace/items, Search indexes or knowledge bases, Redis database, downstream agents, and workplace integrations, only when selected. |
| GitHub publication identity | Target owner/repository and branch or protected environment, with its actual OIDC subject policy. |
| Observability and retention | Required logs, metrics, traces, access controls, retention, and treatment of radio or workplace data. |

Retrieve generated values after provisioning: resource IDs, registry login
server, AKS OIDC issuer, managed identity client and principal IDs, service
endpoints, and hosted-agent identity. Do not derive them from old names or copy
them from an example deployment.

## Architecture and deployment choices

The core voice path is Zello to the Go gateway to Azure OpenAI Realtime, with
spoken responses returned through the gateway. Operational grounding adds an
agent or router and its explicitly selected data sources.

| Component | Requirement |
|---|---|
| Zello Channel API | Required for radio operation. External to Azure provisioning. |
| Go gateway | Required for radio operation. Container includes native audio dependencies and the selected local Whisper model. |
| Azure OpenAI Realtime | Required for cloud voice sessions. Configure a compatible deployment and supported voice. |
| ACR and AKS | Container registry and runtime for the AKS deployment described here. |
| Operator agent or router | Required for the selected grounded-answer route, not for a basic voice-only setup. |
| Foundry project and reasoning model | Required when the chosen agent implementation uses Foundry model or agent APIs. An AKS-hosted agent can still require a Foundry project. |
| Foundry-hosted runtime | Alternative to self-hosting an agent. Do not deploy both variants unless the design needs both. |
| Azure AI Search | Only for a selected Search-backed grounding implementation. Indexing and query permissions are separate responsibilities. |
| Microsoft Fabric | Only for selected ontology, data-agent, or Eventhouse integrations. Requires authorized data, workspace/item access, and applicable capacity. |
| Azure Managed Redis | Only for selected schema or content distribution paths. Verify the chosen reader's behavior when Redis is unavailable. |
| GitHub Actions governance | Optional publication automation. Must be wired for the chosen repository layout. |
| API Management or another AI gateway | Optional model-traffic intermediary. Configure supported authentication and protocol forwarding separately for WebSocket voice and HTTP model requests. |
| Monitoring services | Select according to operational requirements and instrumented code paths. Create them before workloads that reference them. |

Start from the [gateway](../services/mira-gateway/README.md) and
[agent implementations](../services/operator-agent/src/). The repository includes
`operator-persona-agent-aks` and `operator-router-aks`, as well as hosted variants.
The router can depend on downstream specialist agents that are not supplied by
this repository. A fresh Azure deployment does not create those agents or their
data automatically.

**Portability check:** component documentation, manifests, and source defaults
may still contain sample-environment values or fixed integration identities.
Inspect the selected source revision before building. Replace or parameterize
those dependencies in a reviewed change, or mark the route blocked. Rewriting
this guide does not make all existing templates environment-neutral.

## Identity and access

Keep provisioning, image pulling, runtime inference, grounding, and content
publication as separate permission boundaries.

| Principal | Intended access |
|---|---|
| Deployment operator or pipeline | Approved resource creation and configuration. Role-assignment privileges only where required. |
| AKS kubelet identity | Image pull access to the chosen registry. Select the role appropriate to the registry's RBAC or ABAC mode. |
| Gateway workload identity | Realtime inference and any explicitly enabled gateway-side integration. No inherited access to every grounding source. |
| Agent or router workload identity | Its reasoning model and authorized downstream tools/data sources only. |
| Hosted-agent identity | Permissions required by that deployed agent. Retrieve and verify the identity after creation or version changes. |
| Governance publisher identity | Write access to the selected publication destination, not runtime or subscription administration. |

For AKS, enable OIDC and Workload Identity. Annotate each service account with
the managed identity's **client ID** and label the pod template
`azure.workload.identity/use: "true"`. Federation uses the actual cluster issuer,
subject `system:serviceaccount:NAMESPACE:SERVICE_ACCOUNT`, and audience
`api://AzureADTokenExchange`. Use the identity's **principal/object ID** for role
assignments and service access policies.

For GitHub Actions, use issuer `https://token.actions.githubusercontent.com`
and the exact subject generated by the target repository's OIDC configuration.
A standard branch subject is `repo:OWNER/REPO:ref:refs/heads/BRANCH`; protected
environments and customized subject templates differ. Do not assume one subject
format fits every repository.

Azure RBAC can grant management-plane or data-plane permissions depending on the
role. It does not replace Fabric workspace/item access, Redis database access
policies, or downstream application authorization. For each caller, document the
operation, authentication audience, role or policy, and narrowest supported scope.
For direct Azure OpenAI inference, evaluate `Cognitive Services OpenAI User`
at the model resource scope. Validate Foundry, Search, and other roles against
the selected API and current service documentation rather than copying a broad
role list.

Workload identity does not turn a radio username into an authenticated workplace
identity. Delegated Microsoft 365 integrations need separately approved consent,
token custody, and disclosure boundaries. Do not assume the bot can read each
speaker's personal data.

## Runtime configuration

Use the selected revision's
[gateway configuration loader](../services/mira-gateway/internal/config/config.go)
and agent source as the configuration contract. Source defaults are not evidence
of the values serving in an existing environment.

| Gateway setting | Target-specific value or decision |
|---|---|
| `ZELLO_ENDPOINT` | Approved Channel API WebSocket endpoint for the account. |
| `ZELLO_USERNAME`, `ZELLO_PASSWORD`, `ZELLO_AUTH_TOKEN`, `ZELLO_CHANNEL` | Inject from the approved private configuration/secret mechanism. Never embed in an image. |
| `MIRA_WHISPER_MODEL_PATH` | Path to the model packaged or mounted in the image. |
| `AZURE_OPENAI_ENDPOINT` | Model endpoint or supported gateway endpoint from the approved target. |
| `AZURE_OPENAI_REALTIME_DEPLOYMENT` | Target deployment name, not necessarily the underlying model name. |
| `AZURE_OPENAI_REALTIME_VOICE` | Explicit operator selection supported by that deployed model. |
| `MIRA_WAKE_WORD`, `MIRA_CONVERSATION_TIMEOUT` | Explicit wake-word list and conversation duration, reviewed for the intended use. |
| `MIRA_MAX_RX_SECONDS`, `MIRA_MAX_TX_SECONDS` | Receive/transmit limits appropriate to the channel policy. |
| `MIRA_LOOKUP_FILLER` | Reviewed short acknowledgment behavior. Check how the loader treats empty values before attempting to disable it. |
| `FOUNDRY_IQ_DIRECT_ENDPOINT` | Internal Responses endpoint for the selected self-hosted agent. Overrides the hosted-agent route in the gateway. |
| `FOUNDRY_PROJECT_ENDPOINT`, `FOUNDRY_IQ_AGENT_NAME` | Hosted-agent route when no direct endpoint is configured. |
| `FABRIC_KQL_ENDPOINT`, `FABRIC_KQL_DATABASE`, `FABRIC_KQL_TABLE` | Set together only when enabling gateway-side Eventhouse telemetry. |
| `HTTP_LISTEN_ADDR`, `LOG_LEVEL` | Internal health/metrics listener and approved logging verbosity. |

Common agent settings include `FOUNDRY_PROJECT_ENDPOINT` and
`AZURE_AI_MODEL_DEPLOYMENT_NAME`. Depending on the implementation, additional
settings can include `FOUNDRY_IQ_SEARCH_ENDPOINT`, `FOUNDRY_IQ_SEARCH_API_KEY`,
`FABRIC_WORKSPACE_ID`, `REDIS_HOST`, `REDIS_PORT`, and
`APPLICATIONINSIGHTS_CONNECTION_STRING`. These are not a universal agent schema.
Read the chosen entry point and dependencies before rendering configuration.

## Kubernetes and network requirements

Prepare a target-specific overlay outside the source environment's manifests.
Review namespace, service account, image digest, configuration, secret references,
resource requests/limits, probes, storage, and security context before applying it.
Never apply a whole sample directory to the currently selected cluster.

The gateway exposes `/healthz`, `/readyz`, and `/metrics`; verify the configured
listener port. Determine agent health checks from its actual runtime. A running
pod or HTTP 200 alone does not prove model access or correct grounding.

Keep direct agent Services private. The gateway's direct-agent path does not add
Foundry authentication. A ClusterIP alone is not access control: enforce an
appropriate NetworkPolicy and supported network policy engine, or another
authenticated isolation boundary. Limit egress to required services while
preserving DNS, identity token exchange, model calls, and Zello connectivity.

Use non-root containers, least-privilege pod permissions, and read-only filesystems
where supported. Provide explicit writable mounts where required by the runtime,
audio processing, or approved content/token caches. Do not expose credential caches
through diagnostics or shared volumes.

## Grounding and governed content

Provision only the sources selected in the deployment plan. Use synthetic or
approved non-sensitive content for initial validation.

For Fabric, confirm capacity, workspace access, supported item APIs, table bindings,
and the agent's schema expectations. Use supported Fabric APIs or the portal
according to available capabilities and permissions. Azure subscription access
alone does not grant Fabric data access.

For Search, validate the index or knowledge-base contract and supported
authentication method. Prefer identity authentication where the implementation
supports it. If an API key is required, inject it securely with a rotation plan;
do not grant unrelated Search management permissions to the gateway.

For Redis, choose a supported SKU and database configuration, TLS, network access,
and Entra access policy. Separate publication writes from runtime reads wherever
the implementation supports that distinction. Verify schema/key compatibility
against both reader and publisher. Do not assume a cache fallback is implemented
or suitable for production simply because another variant has one.

The [ontology governance guide](../ontology-governance/README.md) describes the
publication tools. Workflow files under `ontology-governance/.github/workflows/`
are not automatically discovered in a monorepo. For this layout, reviewed workflows
must be installed under the repository-root `.github/workflows/`, with working
directories, path filters, and Fabric Git integration paths adjusted accordingly.
Do not create a separate repository unless the operator chooses that design.

## Secret handling and activation

Do not print full environments, Kubernetes Secrets, keys, access tokens,
connection strings, private keys, or delegated token caches. Avoid secrets in
command-line arguments, shell history, rendered manifests, CI output, and images.
Kubernetes Secret base64 encoding is not encryption. Use the organization's
approved secret delivery and access controls.

Provisioning a workload and enabling radio are separate approval points. Keep the
gateway scaled to zero or otherwise verifiably unable to connect until the operator
approves the account, channel, behavior, and activation window. Do not assume a
receive-only switch exists. A connected gateway can respond to someone else's
transmission even if the deployment agent sends no test message.

## Recreation sequence and completion

Follow the [rebuild playbook](REBUILD_PLAYBOOK.md) for the ordered execution gates:
inputs and source review, approved provisioning, identity and data access,
target-specific builds and manifests, offline validation, then separately approved
activation. Record image digests and verification outcomes privately.

The result should be a reproducible target deployment, not a copy of the original
owner's infrastructure. Keep public examples symbolic. If documentation reveals
actual credentials, stop exposing them and request separate credential remediation.
Editing current documentation does not remove earlier content from Git history,
forks, clones, or caches.
