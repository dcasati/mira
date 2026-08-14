# Architecture

This document describes how `mira-gateway` (Zello radio integration) answers
operator questions by grounding on Fabric's Ontology metadata and reading
operational data directly from OneLake, with Azure Managed Redis as a
governed cache in front of Fabric's ontology discovery step.

## Fabric authorization: two incompatible models, and why one of them broke us

There are two separate ways to ground an agent on Fabric data, with two
fundamentally different authorization requirements. Getting this wrong is
what caused the original production outages this architecture replaced.

![Fabric auth scenarios](fabric-auth-scenarios.png)

**Scenario A (broken): the Azure AI Search–wrapped Fabric knowledge base
("Foundry IQ").** The AI Search agentic-retrieval call itself is
authorized normally with the caller's own app-only token
(`Authorization: Bearer <app-only-token>`, scope `search.azure.com/.default`).
But the underlying Fabric ontology knowledge source additionally enforces
query-time ACLs per signed-in user, and requires a **second, separate
token passed via a custom header**:

```
x-ms-query-source-authorization: Bearer <delegated-user-token>
```

If this header is missing, or contains an app-only token instead of a
genuine delegated (human) token, the request is rejected — with generic
errors (`502`/`400`, `TokenExpired`, "invalid null or empty") that give no
indication the real cause is "wrong token type in this header." The only
way to populate this header for a headless service is to mint a delegated
token out of band (e.g. `az account get-access-token --resource
search.azure.com` under a human's session) and inject it manually — we did
this via a k8s secret (`FOUNDRY_IQ_QUERY_SOURCE_TOKEN`) as a stop-gap. It
works, but that token expires (~2.5h observed) and requires perpetual
human re-authentication — not viable for a production, always-on service.

**Scenario B (fixed): Fabric's own native Data Agent MCP endpoint**
(`api.fabric.microsoft.com/v1/mcp/workspaces/{id}/dataagents/{id}/agent`).
This surface has no equivalent second header — it only needs the standard
`Authorization: Bearer <app-only-token>` header (scope
`api.fabric.microsoft.com/.default`). Authorization happens once, out of
band, by granting the calling identity **Member role on the Fabric
workspace** (via the Fabric REST API's `roleAssignments` endpoint, or
Workspace → Manage access in the portal) — not per-request. After that
grant exists, a standard app-only Entra token (managed identity, no human
involved) works every time — the same pattern used for any other headless
Azure-to-Azure call. This is what `operator-agent` and `mira-gateway` use
today: zero delegated tokens, zero `x-ms-query-source-authorization`
header, zero rotation, ever.

## Sequence diagram — one query, end to end

```mermaid
sequenceDiagram
    actor Op as Operator (radio)
    participant Zello as Zello Server
    participant Gw as mira-gateway (Go, AKS)
    participant RT as Azure OpenAI Realtime
    participant Agent as operator-agent-aks (Python)
    participant Redis as Azure Managed Redis
    participant Fabric as Fabric API (Ontology)
    participant OneLake as OneLake (Delta tables)

    Op->>Zello: PTT: "What procedures apply to Sierra 1?"
    Zello->>Gw: audio stream
    Gw->>RT: audio (STT + orchestration)
    RT-->>Gw: tool_call: query_foundry_iq_manuals(question)
    Gw->>Agent: POST /responses {question}

    Agent->>Redis: GET operator_matrix_ontology:schema:v1
    alt cache hit (TTL < 300s)
        Redis-->>Agent: schema JSON (entities, relationships)
    else cache miss / expired / Redis error
        Agent->>Fabric: getDefinition (Ontology API)
        Fabric-->>Agent: schema JSON
    end

    Agent->>OneLake: read Delta tables (ontology-guided joins)
    OneLake-->>Agent: rows (assets, events, procedures...)
    Agent->>Agent: reason over schema + rows, compose grounded answer
    Agent-->>Gw: 200 OK {answer}

    Gw->>RT: tool result
    RT-->>Gw: synthesized speech (TTS)
    Gw->>Zello: send_zello_chat_message (txMinSpacing paced)
    Zello->>Op: radio playback
```

## Architecture diagram — components and the governance pipeline

```mermaid
graph TB
    subgraph Radio["Voice Layer"]
        Op[Operator radios]
        Zello[Zello Server]
    end

    subgraph AKS["AKS cluster: aks-mira-eastus2"]
        GW["mira-gateway<br/>(Go, ns: mira)"]
        Agent["operator-agent-aks<br/>(Python, ns: operator-agent-aks)"]
    end

    subgraph Foundry["Azure AI Foundry"]
        RT[Azure OpenAI Realtime]
        HostedAgent["operator-persona-agent<br/>(hosted agent, parallel deployment)"]
    end

    subgraph DataPlane["Data & Cache"]
        Redis[("Azure Managed Redis<br/>Entra-auth, public endpoint")]
        FabricAPI["Fabric REST API<br/>Ontology getDefinition (fallback)"]
        OneLake[("OneLake<br/>Delta lakehouse tables")]
    end

    subgraph Governance["Governance Pipeline (GitHub)"]
        FabricWS["Fabric workspace<br/>(Ontology item, Git-enabled)"]
        Repo["GitHub repo<br/>operator-matrix-ontology"]
        Actions["GitHub Actions<br/>transform + publish_to_redis.py<br/>(OIDC federated identity)"]
        Enrich["enrichment/synonyms.json<br/>(human-curated, additive)"]
    end

    Op <-->|PTT audio| Zello
    Zello <-->|audio stream| GW
    GW <-->|STT/TTS + tool calls| RT
    GW -->|POST /responses<br/>in-cluster HTTP| Agent
    RT -.->|equivalent path| HostedAgent

    Agent -->|Entra token: redis.azure.com| Redis
    Agent -.->|fallback on cache miss<br/>Entra token: api.fabric.microsoft.com| FabricAPI
    Agent -->|Entra token: storage.azure.com<br/>direct Delta reads| OneLake

    FabricWS -->|Git commit / sync| Repo
    Enrich --> Repo
    Repo -->|push triggers| Actions
    Actions -->|publish schema JSON<br/>Entra ID, no access keys| Redis

    style Redis fill:#c62828,color:#fff
    style OneLake fill:#1565c0,color:#fff
    style FabricAPI fill:#1565c0,color:#fff
    style Actions fill:#2e7d32,color:#fff
```

## Notes

- Redis is a cache **in front of Fabric ontology discovery**, not in the live
  row-data query path — OneLake reads happen directly on every query.
- Fabric API calls only occur on a Redis cache miss (fallback), not on every
  request. Confirmed empirically: across ~76 minutes of live testing, the
  schema was reloaded from Redis only 3 times (matching the 300s TTL), with
  zero Fabric fallbacks.
- The governance pipeline (bottom of the architecture diagram) is fully
  decoupled from the live query path — it only updates what's cached in
  Redis, asynchronously, whenever Fabric's ontology changes.
