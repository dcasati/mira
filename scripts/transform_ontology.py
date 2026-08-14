"""Transform a Fabric-Git-synced Ontology item into the schema shape
operator_matrix_data.py's OntologyGraph expects, merging in any
human-curated enrichment.

Reads from fabric-workspace/<name>.Ontology/ (whatever Fabric's Git
integration named the folder -- discovered by pattern, not hardcoded),
using the exact same file layout as Fabric's getDefinition REST API
(EntityTypes/<id>/definition.json, EntityTypes/<id>/DataBindings/<id>.json,
RelationshipTypes/<id>/definition.json) -- this is intentionally the same
parsing logic as operator_matrix_data.py's parse_ontology(), just reading
from files on disk instead of a getDefinition API response blob, so the
two stay easy to keep in sync if Fabric's ontology definition schema ever
changes.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
FABRIC_WORKSPACE_DIR = REPO_ROOT / "fabric-workspace"
ENRICHMENT_FILE = REPO_ROOT / "enrichment" / "synonyms.json"
SCHEMA_VERSION = "v1"


def find_ontology_dir() -> Path:
    candidates = sorted(FABRIC_WORKSPACE_DIR.glob("*.Ontology"))
    if not candidates:
        raise SystemExit(
            f"No *.Ontology folder found under {FABRIC_WORKSPACE_DIR}. "
            "Has the Fabric workspace been connected via Git integration and committed at least once?"
        )
    if len(candidates) > 1:
        print(f"WARNING: multiple *.Ontology folders found, using {candidates[0].name}", file=sys.stderr)
    return candidates[0]


def load_json(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def parse_ontology(ontology_dir: Path) -> dict:
    entity_names: dict[str, str] = {}
    entities: dict[str, dict] = {}
    relationships: list[dict] = []
    lakehouse_id: str | None = None

    entity_types_dir = ontology_dir / "EntityTypes"
    for entity_dir in sorted(entity_types_dir.iterdir()) if entity_types_dir.is_dir() else []:
        definition_path = entity_dir / "definition.json"
        if not definition_path.exists():
            continue
        obj = load_json(definition_path)
        entity_names[obj["id"]] = obj["name"]

    for entity_dir in sorted(entity_types_dir.iterdir()) if entity_types_dir.is_dir() else []:
        entity_id = entity_dir.name
        name = entity_names.get(entity_id)
        if name is None:
            continue
        bindings_dir = entity_dir / "DataBindings"
        if not bindings_dir.is_dir():
            continue
        for binding_path in sorted(bindings_dir.glob("*.json")):
            obj = load_json(binding_path)
            cfg = obj["dataBindingConfiguration"]
            source = cfg["sourceTableProperties"]
            # Only actually-bound columns are real, queryable columns -- an
            # entity type's own declared `properties` list can contain
            # unbound/leftover properties that don't correspond to any real
            # column (confirmed against this workspace's Ontology: its
            # "Proword" entity type declares 12 properties but only 7 are
            # actually bound to a Lakehouse column).
            columns = [b["sourceColumnName"] for b in cfg["propertyBindings"]]
            entities[name] = {"table": source["sourceTableName"], "columns": columns}
            lakehouse_id = lakehouse_id or source["itemId"]
            # Only need the first binding per entity for our purposes.
            break

    relationship_types_dir = ontology_dir / "RelationshipTypes"
    for rel_dir in sorted(relationship_types_dir.iterdir()) if relationship_types_dir.is_dir() else []:
        definition_path = rel_dir / "definition.json"
        if not definition_path.exists():
            continue
        obj = load_json(definition_path)
        source_name = entity_names.get(obj["source"]["entityTypeId"], obj["source"]["entityTypeId"])
        target_name = entity_names.get(obj["target"]["entityTypeId"], obj["target"]["entityTypeId"])
        relationships.append({"name": obj["name"], "source_entity": source_name, "target_entity": target_name})

    if lakehouse_id is None:
        raise SystemExit("Parsed ontology had no Lakehouse-backed entities -- check the synced definition.")

    return {"lakehouse_id": lakehouse_id, "entities": entities, "relationships": relationships}


def load_enrichment() -> dict:
    if not ENRICHMENT_FILE.exists():
        return {}
    return load_json(ENRICHMENT_FILE)


def merge_enrichment(schema: dict, enrichment: dict) -> dict:
    """Merge human-curated enrichment (e.g. synonyms/aliases for entities)
    into the parsed schema. Enrichment is additive only -- it can attach
    extra context to an entity that already exists from Fabric's own
    metadata, but it can't invent new entities/relationships/tables. That
    keeps Fabric as the single source of truth for structure, while still
    letting a human add curated context via a reviewed PR."""
    entity_synonyms = enrichment.get("entity_synonyms", {})
    for entity_name, synonyms in entity_synonyms.items():
        if entity_name in schema["entities"]:
            schema["entities"][entity_name]["synonyms"] = synonyms
        else:
            print(f"WARNING: enrichment references unknown entity {entity_name!r}, ignoring", file=sys.stderr)
    return schema


def main() -> None:
    ontology_dir = find_ontology_dir()
    schema = parse_ontology(ontology_dir)
    enrichment = load_enrichment()
    schema = merge_enrichment(schema, enrichment)
    schema["schema_version"] = SCHEMA_VERSION

    output_path = REPO_ROOT / "build" / "ontology_schema.json"
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", encoding="utf-8") as f:
        json.dump(schema, f, indent=2, sort_keys=True)

    print(f"Wrote {output_path} ({len(schema['entities'])} entities, {len(schema['relationships'])} relationships)")


if __name__ == "__main__":
    main()
