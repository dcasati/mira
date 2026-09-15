# Mira

**AI copilot for frontline radio operations.** Mira accepts operational
questions over push-to-talk radio and returns concise, grounded spoken answers.

<img src="https://raw.githubusercontent.com/dcasati/mira/main/docs/diagrams/github-gifs/MiraOverviewFlow.gif" alt="App Demo">

**[Watch the full-resolution overview](https://dcasati.github.io/mira/media/MiraOverviewFlow.mp4)** ·
[All walkthroughs](https://dcasati.github.io/mira/)

Start with **“Operator, …”** or **“Dispatcher, …”**. Mira handles the voice
conversation, routes the question to an appropriate source, and returns a concise
spoken answer. Reviewed procedures and live operational evidence keep separate
source and authorization boundaries.

[Architecture](#architecture) · [Animated walkthroughs](#animated-walkthroughs) ·
[Repository layout](#repository-layout) · [Getting started](#getting-started)

## Architecture

[Static overview (PNG)](docs/diagrams/mira-architecture-overview.png) ·
[Static overview (SVG)](docs/diagrams/mira-architecture-overview.svg) ·
[Detailed architecture](docs/diagrams/mira-architecture.png)

The architecture separates three responsibilities:

1. **Voice interaction:** the Go gateway connects Zello to Azure OpenAI Realtime,
   manages the conversation, and delegates operational lookups.
2. **Grounded answers:** the Python router uses reviewed local procedure evidence
   or explicit specialist and live-data tools, depending on the question.
3. **Governed content:** reviewed, versioned snapshots are validated and activated
   independently of application image releases.

<details>
<summary><strong>Detailed architecture and control boundaries</strong></summary>

The detailed view shows procedure retrieval, specialist routing, workplace
identity, alternative Fabric execution modes, and the governed content lifecycle.

[![Detailed Mira reference architecture, including local procedure retrieval, specialist tools, identity boundaries, Fabric execution modes, and the GitHub-to-Redis content release path.](docs/diagrams/mira-architecture.svg)](docs/diagrams/mira-architecture.png)

[Full-size PNG](docs/diagrams/mira-architecture.png) ·
[Scalable SVG](docs/diagrams/mira-architecture.svg)

</details>

## Animated walkthroughs

These videos are rendered directly from the original Remotion compositions at
60 fps. [Open the player](https://dcasati.github.io/mira/) for playback, seeking,
and fullscreen controls. The GIF above contains the complete 52-second
architecture overview; select it to open the full-resolution video.

| Walkthrough | Duration | Content |
|---|---:|---|
| [Full architecture explainer](https://dcasati.github.io/mira/#reference) | 2:08 | Native Remotion scenes covering voice, evidence, routing, and content delivery. |
| [Voice flow](https://dcasati.github.io/mira/#voice) | 0:20 | Radio audio, gateway routing, and the spoken response. |
| [Model gateway](https://dcasati.github.io/mira/#models) | 0:20 | Model inference traffic and direct tool-call boundaries. |
| [Reviewed procedures](https://dcasati.github.io/mira/#procedures) | 0:20 | Local retrieval, cited evidence, and speech rendering. |
| [Evidence loop](https://dcasati.github.io/mira/#evidence) | 0:08 | Repeating evidence signals through the native composition. |
| [Fabric boundary](https://dcasati.github.io/mira/#fabric) | 0:20 | Application tool execution and Fabric grounding routes. |
| [Content release](https://dcasati.github.io/mira/#content) | 0:20 | Reviewed source, release distribution, and local activation. |
| [Architecture diagram overlay](https://dcasati.github.io/mira/#overview) | 0:52 | A separate composition tracing signals over the supplied static diagram. |

## Repository layout

Three co-developed components are versioned together as one reference
architecture:

| Path | What it is | Language |
|---|---|---|
| [`services/mira-gateway/`](services/mira-gateway/) | Zello push-to-talk radio gateway. Handles wake-word detection, Azure OpenAI Realtime conversation sessions, and routes tool calls to the operator agent. | Go |
| [`services/operator-agent/`](services/operator-agent/) | Python operator agent and AKS router, with reviewed procedure retrieval and specialist tools for grounded operational answers. Includes Foundry-hosted and self-hosted variants; see the [agent architecture](services/operator-agent/docs/architecture.md) for component details. | Python |
| [`ontology-governance/`](ontology-governance/) | Governance pipeline: Fabric's Git integration syncs the Ontology item here, GitHub Actions transforms it and publishes it to Azure Managed Redis via Entra ID (no access keys, no stored secrets). This keeps ontology schema changes PR-reviewable instead of silent. | Python (scripts) + GitHub Actions |

## Getting started

Each subfolder is self-contained with its own dependencies, Dockerfile, and
(where applicable) Kubernetes manifests:

- `services/mira-gateway/`: see its own README for build/run instructions,
  including how to produce the local `bin/mira` binary (both the plain
  Go-only build used for unit tests, and the full native build with
  Opus/Whisper support for actually running it).
- `services/operator-agent/`: see `docs/architecture.md` for how grounding
  works, and `docs/slides/` for a technical demo deck and a partner
  go-to-market deck.
- `ontology-governance/`: see its own README for how to connect a Fabric
  workspace's Git integration and how the GitHub Actions publish pipeline
  works.

## Infrastructure

[`docs/INFRASTRUCTURE.md`](docs/INFRASTRUCTURE.md) is a full as-built
snapshot of every Azure resource, identity, RBAC role, and Kubernetes
object behind a live deployment of this system, captured directly from
Azure/`kubectl`, with `az` commands to recreate each piece in a fresh
subscription. Start there if you're standing this up from scratch.

[`docs/REBUILD_PLAYBOOK.md`](docs/REBUILD_PLAYBOOK.md) is written
specifically for an AI coding agent (Copilot, Microsoft Scout, etc.) to
follow step-by-step when asked to rebuild this infrastructure. It specifies
what to ask the user for first, the exact order to create things in, and
where to pause for the handful of steps that are portal-only (Fabric
workspace access, Zello account setup) and can't be automated.
