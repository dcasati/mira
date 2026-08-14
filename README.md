# operator-matrix-ontology

Governance pipeline for the `operator_matrix_ontology` Fabric Ontology item, feeding
the `operator_matrix_lookup` tool used by `operator-persona-agent` (Foundry hosted
agent) and `operator-agent-aks` (AKS self-hosted variant).

## Why this repo exists

The agent needs the ontology's entity/relationship schema (which tables back which
entities, and how entities relate -- e.g. `Event --event_observed_on_asset--> Asset`)
to answer multi-hop operational questions correctly, without hardcoding any of that
domain knowledge in Python.

Previously the agent fetched this schema directly from Fabric's `getDefinition` REST
API at every process startup (~20-27s, no caching, no review gate, no enrichment
point). This repo adds:

1. **Governance** -- ontology changes flow through Fabric's own Git integration into
   this repo as a diffable, PR-reviewable commit before ever reaching a live agent.
2. **Enrichment** -- `enrichment/synonyms.json` lets a human curate additional
   context (aliases, extra descriptions) Fabric's raw structural ontology doesn't
   capture, versioned and reviewed like any other change.
3. **Fast, shared serving** -- the transformed schema is published to Azure Managed
   Redis (private-endpoint-only, Entra ID auth) so every agent replica gets an
   instant, pre-baked schema on startup instead of independently re-fetching from
   Fabric.

This pipeline governs **schema only** (the ontology's structure). Live operational
row data (asset status, events, missions) is intentionally NOT cached here -- the
agent reads that directly from OneLake on every call (with its own short TTL cache),
because that data changes far more often than the schema does and Redis, fed by a
CI pipeline, has a much coarser freshness floor than a live table read.

## How it works

1. **Fabric -> GitHub**: the `operator_matrix_ontology` workspace is connected to
   this repo via Fabric's Git integration (configured directly in the Fabric portal,
   pointed at the `fabric-workspace/` folder). Committing from Fabric syncs the
   ontology's raw definition (`EntityTypes/`, `RelationshipTypes/`, `.platform` --
   the same shape as Fabric's `getDefinition` API) into `fabric-workspace/`.
2. **GitHub Actions** (`.github/workflows/publish-ontology.yml`), triggered on push
   to `main` touching `fabric-workspace/**` or `enrichment/**`:
   - Runs `scripts/transform_ontology.py`, which parses the raw Fabric definition
     into the same entity/table/relationship shape the agent's own
     `operator_matrix_data.py` builds, merges in `enrichment/synonyms.json`, and
     validates the result.
   - Runs `scripts/publish_to_redis.py`, which authenticates to Azure Managed Redis
     using Entra ID (via OIDC federation -- no stored secret) and publishes the
     transformed schema as a single JSON value.
3. **Agent-side**: `operator-persona-agent` / `operator-agent-aks` try Redis first
   at startup. If Redis is empty or unreachable, they fall back to the existing
   direct Fabric `getDefinition` discovery -- Redis is a cache, never a hard
   dependency.

## Redis key

`operator_matrix_ontology:schema:v1` -- a single JSON blob containing the parsed
entities, table bindings, relationships, and any enrichment. Versioned in the key
name so a future breaking schema-shape change can coexist during rollout.
