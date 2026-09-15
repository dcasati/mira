# Mira

**AI copilot for frontline radio operations.** Mira accepts operational
questions over push-to-talk radio and returns concise, grounded spoken answers.

<img src="https://raw.githubusercontent.com/dcasati/mira/main/docs/diagrams/github-gifs/MiraOverviewFlow.gif" alt="Animated MIRA architecture overview: radio interaction, grounded answers, and governed content">

Start with **“Operator, …”** or **“Dispatcher, …”**. Mira handles the voice
conversation, routes the question to an appropriate source, and returns a concise
spoken answer. Reviewed procedures and live operational evidence keep separate
source and authorization boundaries.

## Target audience

Platform engineers and application developers integrating radio workflows with
Azure AI and operational data, and operations teams evaluating voice access to
reviewed procedures and authorized workplace information.

## What MIRA does

| Capability | Responsibility |
|---|---|
| Radio interaction | Receive Zello transmissions, detect wake words locally, and return spoken responses. |
| Source-aware answers | Route questions to reviewed procedure evidence or configured specialist and live-data tools. |
| Operational grounding | Keep procedure evidence, workplace information, and Fabric query results within their respective authorization boundaries. |
| Governed content | Review and version content separately from application code and container releases. |

## Architecture

The architecture separates three responsibilities:

1. **Voice interaction:** the Go gateway connects Zello to Azure OpenAI Realtime,
   manages the conversation, and delegates operational lookups.
2. **Grounded answers:** the Python router uses reviewed local procedure evidence
   or explicit specialist and live-data tools, depending on the question.
3. **Governed content:** reviewed, versioned snapshots are validated and activated
   independently of application image releases.

### Detailed architecture and control boundaries

The detailed view shows procedure retrieval, specialist routing, workplace
identity, alternative Fabric execution modes, and the governed content lifecycle.

[![Detailed Mira reference architecture, including local procedure retrieval, specialist tools, identity boundaries, Fabric execution modes, and the GitHub-to-Redis content release path.](docs/diagrams/mira-architecture.svg)](docs/diagrams/mira-architecture.png)

## Solution components

| Component | Role |
|---|---|
| [Zello Channel API](https://developers.zello.com/) | Push-to-talk audio transport. |
| [Azure Kubernetes Service](https://learn.microsoft.com/en-us/azure/aks/) | Container runtime for the Go gateway and Python router. |
| [Azure OpenAI Realtime](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/realtime-audio) | Voice-model sessions and spoken response generation. |
| [Microsoft Entra Workload ID](https://learn.microsoft.com/en-us/azure/aks/workload-identity-overview) | Azure workload authentication without embedding identity credentials in application images. |
| [Microsoft Fabric](https://learn.microsoft.com/en-us/fabric/) | Operational grounding for configured Fabric integrations. |
| [Azure Managed Redis](https://learn.microsoft.com/en-us/azure/redis/) | Governed schema and content distribution, separate from live operational queries. |
| [GitHub Actions](https://docs.github.com/en/actions) | Automated publication of reviewed ontology changes. |

## Repository layout

Application services and governance content:

| Path | What it is | Language |
|---|---|---|
| [`services/mira-gateway/`](services/mira-gateway/) | Zello push-to-talk radio gateway. Handles wake-word detection, Azure OpenAI Realtime conversation sessions, and routes tool calls to the operator agent. | Go |
| [`services/operator-agent/`](services/operator-agent/) | Python operator agent and AKS router, with reviewed procedure retrieval and specialist tools for grounded operational answers. Includes Foundry-hosted and self-hosted variants; see the [agent architecture](services/operator-agent/docs/architecture.md) for component details. | Python |
| [`ontology-governance/`](ontology-governance/) | Governance pipeline: Fabric's Git integration syncs the Ontology item here, GitHub Actions transforms it and publishes it to Azure Managed Redis via Entra ID (no access keys, no stored secrets). This keeps ontology schema changes PR-reviewable instead of silent. | Python (scripts) + GitHub Actions |

## Getting started

### Before you begin

- Git, Docker, Azure CLI, and `kubectl`.
- Go 1.25 or later for gateway development, plus the Python dependencies for
  the selected operator-agent variant.
- An Azure subscription, an AKS cluster, and access to a deployed Realtime model.
- A dedicated Zello account, channel access, and Channel API authorization.
- Permissions for each data integration you enable. Fabric workspace access
  is required for Fabric grounding, not for every radio interaction.

Full native gateway builds also require libopus, whisper.cpp, and a Whisper
model. The [gateway prerequisites](services/mira-gateway/README.md#prerequisites)
describe the local and container build requirements. Keep credentials out of Git.

### Clone the repository

```bash
git clone https://github.com/dcasati/mira.git
cd mira
```

### Configure the components

| Step | Guide |
|---|---|
| 1. Set up radio and Realtime access | [Gateway setup and build instructions](services/mira-gateway/README.md). |
| 2. Select the operator-agent implementation and grounding route | [Agent implementations](services/operator-agent/) and [grounding architecture](services/operator-agent/docs/architecture.md). |
| 3. Configure ontology publication if using Fabric ontology grounding | [Ontology governance](ontology-governance/README.md). |

Follow the component-specific configuration and deployment instructions. These
are separate services, not a single-command installation.

## Infrastructure

| Guide | Purpose |
|---|---|
| [Infrastructure inventory](docs/INFRASTRUCTURE.md) | Azure resources, identities, role assignments, and Kubernetes configuration. |
| [Rebuild playbook](docs/REBUILD_PLAYBOOK.md) | Ordered setup steps, required inputs, and manual approval points. |
