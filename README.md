# Mira

**AI copilot for frontline radio operations.** Mira lets a worker ask an
operational question over an ordinary push-to-talk radio and get a spoken,
grounded answer back — hands-free, no app, no screen.

This repo merges three previously-separate repositories into one, since they
are co-developed and versioned together as a single reference architecture:

| Path | What it is | Language |
|---|---|---|
| [`services/mira-gateway/`](services/mira-gateway/) | Zello push-to-talk radio gateway. Handles wake-word detection, Azure OpenAI Realtime conversation sessions, and routes tool calls to the operator agent. | Go |
| [`services/operator-agent/`](services/operator-agent/) | Ontology-grounded operator agent. Discovers schema from Microsoft Fabric's Ontology (via a governed Redis cache, falling back to live Fabric discovery), then reads operational data directly from OneLake. Deployed both as an Azure AI Foundry hosted agent and as a self-hosted AKS variant. See [`docs/architecture.md`](services/operator-agent/docs/architecture.md) for the full sequence/architecture diagrams. | Python |
| [`ontology-governance/`](ontology-governance/) | Governance pipeline: Fabric's Git integration syncs the Ontology item here, GitHub Actions transforms it and publishes it to Azure Managed Redis via Entra ID (no access keys, no stored secrets). This keeps ontology schema changes PR-reviewable instead of silent. | Python (scripts) + GitHub Actions |

## Why one repo

`mira-gateway` and `operator-agent` change together constantly — a routing
change on one side usually needs a matching tool-schema change on the other.
Keeping them in one repo means those changes land in a single, atomic PR
instead of being coordinated across separate repos.

`ontology-governance` is included here as the current reference example of
the governance pattern, but note that in a real multi-customer deployment
each customer would have **their own** ontology-governance content (their
own Fabric workspace, their own curated enrichment) while reusing the same
`mira-gateway` + `operator-agent` code unchanged.

## Getting started

Each subfolder is self-contained with its own dependencies, Dockerfile, and
(where applicable) Kubernetes manifests:

- `services/mira-gateway/` — see its own README for build/run instructions.
- `services/operator-agent/` — see `docs/architecture.md` for how grounding
  works, and `docs/slides/` for a technical demo deck and a partner
  go-to-market deck.
- `ontology-governance/` — see its own README for how to connect a Fabric
  workspace's Git integration and how the GitHub Actions publish pipeline
  works.

## Infrastructure

[`docs/INFRASTRUCTURE.md`](docs/INFRASTRUCTURE.md) is a full as-built
snapshot of every Azure resource, identity, RBAC role, and Kubernetes
object behind a live deployment of this system — captured directly from
Azure/`kubectl`, with `az` commands to recreate each piece in a fresh
subscription. Start there if you're standing this up from scratch.

[`docs/REBUILD_PLAYBOOK.md`](docs/REBUILD_PLAYBOOK.md) is written
specifically for an AI coding agent (Copilot, Microsoft Scout, etc.) to
follow step-by-step when asked to rebuild this infrastructure — it says
what to ask the user for first, the exact order to create things in, and
where to pause for the handful of steps that are portal-only (Fabric
workspace access, Zello account setup) and can't be automated.

## History note

This repo was formed by merging three prior repositories
(`dcasati/mira`, `dcasati/mira-operator-agent`,
`dcasati/operator-matrix-ontology`) via `git subtree`, preserving each
one's full commit history under its new path.
