# Copyright (c) Microsoft. All rights reserved.
#
# Direct OneLake/Delta-table reader for Fabric operational data, driven
# entirely by Fabric's own Ontology metadata -- no hardcoded table names,
# column names, entity relationships, or domain-specific synonym mappings
# anywhere in this file.
#
# BACKGROUND
#
# Live testing (App Insights traces on operator-persona-agent, 2026-08-13)
# showed that a "Sierra 1" callsign lookup through the Fabric Data Agent's
# own native MCP endpoint took ~32 of a ~42s total tool-call span -- the
# Data Agent's own internal NL-to-query LLM reasoning, not Fabric capacity
# queueing or network transit (confirmed: the identical ~55-58s range
# reproduced live in Foundry's OWN playground, entirely within Foundry/
# Fabric's East US 2 infrastructure). Reading the underlying Delta tables
# directly replaced that ~32s hop with a plain OneLake read: ~1.4-1.7s
# steady-state cold, effectively instant once cached.
#
# A FIRST VERSION of this module (still in git history / earlier session
# state) replaced the Data Agent's NL-to-query reasoning with a small
# hand-written keyword/synonym router (e.g. "relay hardline" -> asset ID
# "ASSET-004"). That's real hardcoded domain knowledge duplicated from
# Fabric's own metadata, and it can't answer genuine multi-hop questions
# like "what procedures apply to the asset that Sierra 1 last reported on"
# at all -- there's no keyword list that expresses a 3-hop graph traversal.
#
# THIS VERSION replaces that router entirely. Fabric's workspace has a
# real Ontology item (type "Ontology", e.g. "operator_matrix_ontology")
# with explicit EntityTypes, DataBindings (which Lakehouse table/columns
# back each entity), and RelationshipTypes (typed edges between entities,
# e.g. "Event --event_observed_on_asset--> Asset"). That's the actual,
# authoritative graph Fabric's own Data Agent would have reasoned over --
# we can read it directly via the same Fabric REST API
# (getDefinition) used to inspect the Data Agent's own definition earlier
# this session, at startup, and use it to:
#   1. Discover which Lakehouse and tables actually back the ontology
#      (from DataBindings' sourceTableProperties), instead of a hardcoded
#      table list.
#   2. Build a human-readable entity/relationship schema description --
#      generated from Fabric's own metadata, not written by hand -- and
#      attach it to the operator_matrix_lookup tool's own `description`,
#      which the model sees on every turn as part of its tool list.
#   3. Return the full contents of every relevant table on each lookup
#      (the whole dataset is a few dozen rows total -- trivial to hand to
#      the model directly) so the model itself performs the actual
#      multi-hop graph reasoning, guided by the real relationship schema,
#      rather than us pre-filtering with guessed keyword logic.
#
# The only required configuration is FABRIC_WORKSPACE_ID -- which
# workspace to look at is a legitimate deployment parameter, not domain
# knowledge. The Ontology item, its backing Lakehouse, its tables, its
# entity/relationship schema, and any extra tables not covered by the
# ontology (e.g. join tables the ontology autoencoding doesn't explicitly
# bind, discovered via a plain OneLake directory listing) are all
# discovered live, every time this process starts.
#
# Auth: the same app-only workload identity already used for OneLake reads
# (https://storage.azure.com/.default) plus one additional scope,
# https://api.fabric.microsoft.com/.default, for the Fabric control-plane
# REST calls (list workspace items, get item definition). Both use the
# SAME underlying identity/Member-role grant on the Fabric workspace --
# confirmed by direct testing that no additional RBAC was needed.
#
# GOVERNANCE / FRESHNESS (added after initial deployment)
#
# The Ontology schema (entities/relationships) is fetched once per
# SCHEMA_TTL_SECONDS, sourced from Azure Managed Redis when REDIS_HOST is
# configured, falling back to live Fabric discovery otherwise. Redis is
# fed by a separate, PR-reviewable CI pipeline
# (github.com/dcasati/operator-matrix-ontology): Fabric's own Git
# integration syncs the Ontology's definition into that repo on every
# commit from the Fabric side, GitHub Actions transforms it (optionally
# merging in human-curated enrichment) and publishes it to Redis using
# Entra ID auth (no access keys, no stored secret -- OIDC federation from
# GitHub Actions to Azure). This exists so ontology changes are reviewed
# before reaching a live agent, and so every agent replica gets an
# instant, pre-baked schema instead of independently re-running Fabric's
# slow (~20-27s, LRO-polled) getDefinition discovery.
#
# Redis is a cache, never a hard dependency: any failure reading from it
# (unset, unreachable, empty, malformed) transparently falls back to the
# direct Fabric discovery this module always supported. Table ROW DATA
# (missions, assets, events, ...) is never cached in Redis -- it's read
# directly from OneLake on its own short TTL (CACHE_TTL_SECONDS), because
# operational data changes far more often than the schema does and a
# CI-published cache has a much coarser freshness floor than a live read.

from __future__ import annotations

import logging
import time
from dataclasses import dataclass, field

import httpx
import pandas as pd
import redis
from azure.core.credentials import TokenCredential
from deltalake import DeltaTable

logger = logging.getLogger(__name__)

FABRIC_API_BASE = "https://api.fabric.microsoft.com/v1"
FABRIC_API_SCOPE = "https://api.fabric.microsoft.com/.default"
ONELAKE_HOST = "onelake.dfs.fabric.microsoft.com"
ONELAKE_SCOPE = "https://storage.azure.com/.default"
REDIS_SCOPE = "https://redis.azure.com/.default"
REDIS_SCHEMA_KEY = "operator_matrix_ontology:schema:v1"

# Hard wall-clock ceiling on the entire Redis read attempt (token
# acquisition + DNS + TCP connect + GET), enforced via a worker thread
# rather than relying solely on redis-py's socket_timeout/
# socket_connect_timeout -- those bound the TCP connect/read itself but
# NOT DNS resolution, which can hang indefinitely under a broken/slow
# resolver with no enforced upper bound otherwise. Redis is only ever a
# speed optimization over the Fabric fallback, so it must never be
# allowed to take anywhere near as long as that fallback, let alone hang.
_REDIS_HARD_TIMEOUT_SECONDS = 8.0

# Cache freshness window for table row data.
CACHE_TTL_SECONDS = 300

# Refresh window for the ontology schema itself. Unlike row data, this is
# fetched from Redis (a governed, CI-published cache -- see the
# operator-matrix-ontology repo) when available, which is cheap enough
# (a single GET, no Fabric LRO polling) to refresh on the same cadence as
# row data rather than only once at process startup. If Redis is
# unreachable or empty, this falls back to the direct Fabric getDefinition
# discovery (~20-27s, LRO-polled) and that result is cached for the same
# window before being retried.
SCHEMA_TTL_SECONDS = 300

_LRO_POLL_INTERVAL_SECONDS = 1.0
_LRO_TIMEOUT_SECONDS = 60.0


@dataclass
class _EntityInfo:
    table: str
    columns: list[str]


@dataclass
class _Relationship:
    name: str
    source_entity: str
    target_entity: str


@dataclass
class OntologyGraph:
    """Parsed Fabric Ontology metadata: which entities exist, which
    Lakehouse table/columns back each one, and how entities relate."""

    lakehouse_id: str
    entities: dict[str, _EntityInfo] = field(default_factory=dict)
    relationships: list[_Relationship] = field(default_factory=list)

    def bound_tables(self) -> set[str]:
        return {info.table for info in self.entities.values()}

    def describe(self) -> str:
        """Render a human-readable entity/relationship schema, generated
        entirely from the parsed Fabric metadata, for use as the lookup
        tool's own description (so the model always has the real
        relationship graph available for multi-hop reasoning)."""
        lines: list[str] = ["Entities (Fabric Ontology, discovered live):"]
        for name, info in sorted(self.entities.items()):
            lines.append(f"- {name} (table: {info.table}): {', '.join(info.columns)}")
        lines.append("")
        lines.append("Relationships (use these to chain across tables for multi-hop questions):")
        for rel in self.relationships:
            lines.append(f"- {rel.source_entity} --{rel.name}--> {rel.target_entity}")
        return "\n".join(lines)


def _fabric_get(client: httpx.Client, token: str, path: str) -> httpx.Response:
    resp = client.get(f"{FABRIC_API_BASE}{path}", headers={"Authorization": f"Bearer {token}"})
    resp.raise_for_status()
    return resp


def _fabric_post_lro(client: httpx.Client, token: str, path: str) -> dict:
    """POST to a Fabric long-running-operation endpoint (e.g.
    getDefinition) and return the final JSON result, transparently
    handling both an immediate 200 and the more common 202 + poll-the-
    Location-header + fetch-/result pattern."""
    resp = client.post(f"{FABRIC_API_BASE}{path}", headers={"Authorization": f"Bearer {token}"}, json={})
    resp.raise_for_status()
    if resp.status_code == 200:
        return resp.json()

    location = resp.headers["Location"]
    deadline = time.monotonic() + _LRO_TIMEOUT_SECONDS
    while True:
        status_resp = client.get(location, headers={"Authorization": f"Bearer {token}"})
        status_resp.raise_for_status()
        status = status_resp.json().get("status")
        if status == "Succeeded":
            break
        if status == "Failed":
            raise RuntimeError(f"Fabric long-running operation failed: {status_resp.json()}")
        if time.monotonic() > deadline:
            raise TimeoutError(f"Fabric long-running operation timed out: {location}")
        time.sleep(_LRO_POLL_INTERVAL_SECONDS)

    result_resp = client.get(f"{location}/result", headers={"Authorization": f"Bearer {token}"})
    result_resp.raise_for_status()
    return result_resp.json()


def _discover_ontology_item_id(client: httpx.Client, token: str, workspace_id: str) -> str:
    items = _fabric_get(client, token, f"/workspaces/{workspace_id}/items").json().get("value", [])
    ontology_items = [item for item in items if item.get("type") == "Ontology"]
    if not ontology_items:
        raise RuntimeError(f"No Fabric Ontology item found in workspace {workspace_id}")
    if len(ontology_items) > 1:
        logger.warning(
            "operator_matrix.multiple_ontologies_found workspace=%s using=%s",
            workspace_id,
            ontology_items[0]["displayName"],
        )
    return ontology_items[0]["id"]


def _fetch_ontology_definition(client: httpx.Client, token: str, workspace_id: str, ontology_item_id: str) -> dict:
    return _fabric_post_lro(client, token, f"/workspaces/{workspace_id}/items/{ontology_item_id}/getDefinition")


def _decode_parts(definition: dict) -> dict[str, dict]:
    """Base64-decode and JSON-parse every part of a Fabric item definition,
    keyed by its path."""
    import base64
    import json

    return {
        part["path"]: json.loads(base64.b64decode(part["payload"]).decode("utf-8"))
        for part in definition["definition"]["parts"]
    }


def parse_ontology(definition: dict) -> OntologyGraph:
    parts = _decode_parts(definition)

    entity_names: dict[str, str] = {}
    lakehouse_id: str | None = None
    entities: dict[str, _EntityInfo] = {}
    relationships: list[_Relationship] = []

    for path, obj in parts.items():
        if "EntityTypes" in path and path.endswith("definition.json") and "DataBindings" not in path:
            entity_names[obj["id"]] = obj["name"]

    for path, obj in parts.items():
        if "DataBindings" not in path:
            continue
        entity_id = path.split("/")[1]
        name = entity_names.get(entity_id)
        if name is None:
            continue
        cfg = obj["dataBindingConfiguration"]
        source = cfg["sourceTableProperties"]
        # Only actually-bound columns are real, queryable columns -- an
        # entity type's own declared `properties` list can contain
        # unbound/leftover properties that don't correspond to any real
        # column (confirmed directly against this workspace's Ontology:
        # its "Proword" entity type declares 12 properties but only 7 are
        # actually bound to a Lakehouse column).
        columns = [binding["sourceColumnName"] for binding in cfg["propertyBindings"]]
        entities[name] = _EntityInfo(table=source["sourceTableName"], columns=columns)
        lakehouse_id = lakehouse_id or source["itemId"]

    for path, obj in parts.items():
        if "RelationshipTypes" in path and path.endswith("definition.json"):
            source_name = entity_names.get(obj["source"]["entityTypeId"], obj["source"]["entityTypeId"])
            target_name = entity_names.get(obj["target"]["entityTypeId"], obj["target"]["entityTypeId"])
            relationships.append(_Relationship(name=obj["name"], source_entity=source_name, target_entity=target_name))

    if lakehouse_id is None:
        raise RuntimeError("Fabric Ontology definition had no Lakehouse-backed entities")

    return OntologyGraph(lakehouse_id=lakehouse_id, entities=entities, relationships=relationships)


def _decode_jwt_claim(token: str, claim: str) -> str:
    """Extract a claim from a JWT's payload without verifying its
    signature -- safe here because the token was obtained directly from
    our own trusted credential (never from an untrusted source), and we
    only need the "oid" (object ID) claim to use as the Redis username
    per Microsoft's documented Entra ID auth pattern for Azure Managed
    Redis (username = object ID of the calling identity, password = the
    Entra token itself). Avoids taking on a JWT-decoding library
    dependency for this one field."""
    import base64
    import json

    payload_b64 = token.split(".")[1]
    padded = payload_b64 + "=" * (-len(payload_b64) % 4)
    payload = json.loads(base64.urlsafe_b64decode(padded))
    return payload[claim]


def _graph_from_redis_json(data: dict) -> OntologyGraph:
    entities = {name: _EntityInfo(table=info["table"], columns=info["columns"]) for name, info in data["entities"].items()}
    relationships = [
        _Relationship(name=r["name"], source_entity=r["source_entity"], target_entity=r["target_entity"])
        for r in data["relationships"]
    ]
    return OntologyGraph(lakehouse_id=data["lakehouse_id"], entities=entities, relationships=relationships)


def list_lakehouse_tables(client: httpx.Client, token: str, workspace_id: str, lakehouse_id: str) -> list[str]:
    """List every table actually present in the Lakehouse via a plain
    OneLake (ADLS Gen2) directory listing -- this picks up tables that
    exist in the data but aren't bound to any Ontology entity (e.g. this
    workspace's mission_events, a join table backing the
    "Mission --mission_has_event--> Event" relationship type without
    itself being bound to a named entity)."""
    resp = client.get(
        f"https://{ONELAKE_HOST}/{workspace_id}",
        params={"resource": "filesystem", "recursive": "false", "directory": f"{lakehouse_id}/Tables"},
        headers={"Authorization": f"Bearer {token}", "x-ms-version": "2023-11-03"},
    )
    resp.raise_for_status()
    prefix = f"{lakehouse_id}/Tables/"
    return [
        path["name"][len(prefix) :]
        for path in resp.json().get("paths", [])
        if path.get("isDirectory") == "true" and path["name"].startswith(prefix)
    ]


class OperatorMatrixStore:
    """In-memory, TTL-cached direct reader for Fabric operational data.

    Schema (entities, table bindings, relationships) is loaded from Redis
    when available (REDIS_HOST set) -- a governed, CI-published cache fed
    by Fabric's Git integration + GitHub Actions (see the
    operator-matrix-ontology repo) -- refreshed on the same TTL cadence as
    row data since a Redis GET is cheap. If Redis is unset, empty, or
    unreachable, this falls back to discovering the schema directly from
    Fabric's own getDefinition REST API (~20-27s, LRO-polled) -- Redis is
    always a cache, never a hard dependency.

    Table row data is always read directly from OneLake regardless of
    where the schema came from -- Redis never caches row data, since that
    changes far more often than the schema does and a CI-published cache
    has a much coarser freshness floor than a live table read."""

    def __init__(
        self,
        credential: TokenCredential,
        workspace_id: str,
        redis_host: str | None = None,
        redis_port: int = 10000,
    ) -> None:
        self._credential = credential
        self._workspace_id = workspace_id
        self._redis_host = redis_host
        self._redis_port = redis_port
        self._table_cache: dict[str, tuple[float, pd.DataFrame]] = {}
        self._graph: OntologyGraph | None = None
        self._graph_loaded_at: float = 0.0
        self._tables: list[str] = []

    def _fabric_token(self) -> str:
        return self._credential.get_token(FABRIC_API_SCOPE).token

    def _onelake_token(self) -> str:
        return self._credential.get_token(ONELAKE_SCOPE).token

    def _table_uri(self, table: str) -> str:
        return f"abfss://{self._workspace_id}@{ONELAKE_HOST}/{self._graph.lakehouse_id}/Tables/{table}"

    def _get_table(self, table: str) -> pd.DataFrame:
        now = time.monotonic()
        cached = self._table_cache.get(table)
        if cached is not None and now - cached[0] < CACHE_TTL_SECONDS:
            return cached[1]
        token = self._onelake_token()
        dt = DeltaTable(self._table_uri(table), storage_options={"bearer_token": token, "use_fabric_endpoint": "true"})
        df = dt.to_pandas()
        self._table_cache[table] = (now, df)
        return df

    def _load_graph_from_redis(self) -> OntologyGraph | None:
        if not self._redis_host:
            return None
        # Bounded by a hard wall-clock timeout via a worker thread: Python's
        # socket_timeout/socket_connect_timeout on redis.Redis() only bound
        # the TCP connect/read itself, NOT DNS resolution (getaddrinfo),
        # which can hang indefinitely under a broken/slow resolver with no
        # enforced upper bound. Since this whole thing is only ever a
        # "nice to have faster than Fabric" optimization -- never a hard
        # dependency -- it must never be allowed to take longer than (or
        # even close to) the Fabric fallback path it's meant to speed up.
        #
        # Deliberately NOT using the executor as a context manager: `with
        # ThreadPoolExecutor(...) as pool:` calls pool.shutdown(wait=True)
        # on exit, which blocks until the worker thread finishes -- if
        # that thread is genuinely stuck (e.g. a DNS hang with no
        # enforced timeout), that would silently defeat the whole point
        # of this hard timeout by hanging on exit anyway. shutdown(wait=
        # False) lets this function return promptly regardless of
        # whether the worker thread ever completes.
        import concurrent.futures

        executor = concurrent.futures.ThreadPoolExecutor(max_workers=1)
        future = executor.submit(self._load_graph_from_redis_unbounded)
        try:
            return future.result(timeout=_REDIS_HARD_TIMEOUT_SECONDS)
        except Exception:
            logger.exception("operator_matrix.redis_read_failed_or_timed_out")
            return None
        finally:
            executor.shutdown(wait=False)

    def _load_graph_from_redis_unbounded(self) -> OntologyGraph | None:
        token = self._credential.get_token(REDIS_SCOPE).token
        object_id = _decode_jwt_claim(token, "oid")
        client = redis.Redis(
            host=self._redis_host,
            port=self._redis_port,
            ssl=True,
            username=object_id,
            password=token,
            decode_responses=True,
            socket_timeout=5.0,
            socket_connect_timeout=5.0,
        )
        try:
            raw = client.get(REDIS_SCHEMA_KEY)
        finally:
            client.close()
        if raw is None:
            logger.warning("operator_matrix.redis_key_missing key=%s", REDIS_SCHEMA_KEY)
            return None
        import json

        return _graph_from_redis_json(json.loads(raw))

    def _discover_graph_from_fabric(self, client: httpx.Client) -> OntologyGraph:
        fabric_token = self._fabric_token()
        ontology_item_id = _discover_ontology_item_id(client, fabric_token, self._workspace_id)
        definition = _fetch_ontology_definition(client, fabric_token, self._workspace_id, ontology_item_id)
        return parse_ontology(definition)

    def _ensure_graph(self) -> None:
        now = time.monotonic()
        if self._graph is not None and now - self._graph_loaded_at < SCHEMA_TTL_SECONDS:
            return

        graph = self._load_graph_from_redis()
        source = "redis"
        if graph is None:
            with httpx.Client(timeout=30.0) as client:
                graph = self._discover_graph_from_fabric(client)
            source = "fabric"

        with httpx.Client(timeout=30.0) as client:
            onelake_token = self._onelake_token()
            discovered = list_lakehouse_tables(client, onelake_token, self._workspace_id, graph.lakehouse_id)
            # Union of ontology-bound tables and every table actually
            # present in the Lakehouse -- covers join/relationship tables
            # (like mission_events) the Ontology doesn't bind to a named
            # entity, without hardcoding any specific table name. This
            # OneLake listing always runs live (never Redis-cached) --
            # it's already fast and the agent needs live OneLake access
            # for row data regardless.
            self._tables = sorted(set(discovered) | graph.bound_tables())

        self._graph = graph
        self._graph_loaded_at = now
        logger.info(
            "operator_matrix.schema_loaded source=%s lakehouse=%s entities=%d relationships=%d tables=%d",
            source,
            graph.lakehouse_id,
            len(graph.entities),
            len(graph.relationships),
            len(self._tables),
        )

    def warm_cache(self) -> None:
        """Load the schema (Redis first, falling back to live Fabric
        discovery) and pre-fetch every table's contents, all at process
        startup -- so the first real question pays no discovery or
        OneLake round-trip latency at all."""
        self._ensure_graph()

        for table in self._tables:
            try:
                self._get_table(table)
                logger.info("operator_matrix.warmed table=%s", table)
            except Exception:
                logger.exception("operator_matrix.warm_failed table=%s", table)

    def schema_description(self) -> str:
        if self._graph is None:
            return "Schema not yet discovered."
        extra = sorted(set(self._tables) - self._graph.bound_tables())
        text = self._graph.describe()
        if extra:
            text += "\n\nAdditional tables present (not bound to a named entity, e.g. relationship join tables):\n"
            text += "\n".join(f"- {table}" for table in extra)
        return text

    def lookup(self, question: str) -> str:
        """Return the full current contents of every discovered table.
        The dataset is small (a few dozen rows total across all tables),
        so rather than pre-filtering with guessed keyword logic, the
        whole thing is handed to the calling model every time -- guided
        by the real relationship schema in this tool's own description,
        the model performs the actual (potentially multi-hop) reasoning
        itself.

        Refreshes the schema (Redis first, TTL-gated -- see _ensure_graph)
        before every call and prepends it to the result, so the model
        always sees the current entity/relationship schema on every
        invocation, not just whatever was true when this tool's static
        `description` was registered at agent-construction time (which
        does NOT get updated by a later background schema refresh)."""
        del question  # kept for interface stability / call tracing only
        self._ensure_graph()
        sections: list[str] = [self.schema_description()]
        for table in self._tables:
            try:
                df = self._get_table(table)
            except Exception as exc:
                logger.exception("operator_matrix.lookup_failed table=%s", table)
                sections.append(f"[{table}] read failed: {exc}")
                continue
            rows = [row.to_dict() for _, row in df.iterrows()]
            rows_text = "\n".join(str(r) for r in rows)
            sections.append(f"[{table}] {len(rows)} row(s):\n{rows_text}")
        return "\n\n".join(sections)
