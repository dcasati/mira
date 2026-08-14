# Fabric IQ Matrix-style POC package

This package contains original synthetic data for a Matrix-inspired Operator demo. It does not reproduce story text, dialogue, or proprietary content from any film; it uses fictional simulation-operations concepts for testing Fabric IQ, Fabric data agents, and Foundry agent grounding.

## Files

| File | Purpose |
|---|---|
| `operator_mission_manual.csv` | Procedure-style records for Operator to retrieve as structured "documents" |
| `simulation_assets.csv` | Fictional assets, nodes, checkpoints, and signal sites |
| `operator_events.csv` | Time-series event log for KQL/lakehouse testing |
| `call_signs.csv` | Call signs, units, NATO phonetic aliases, and operator phrases |
| `missions.csv` | Mission objectives, assignments, target assets, and linked procedures |
| `mission_events.csv` | Many-to-many relationship between missions and events |
| `original_roles.csv` | Original role/persona records for Matrix-style storytelling without copyrighted character names |
| `ontology/operator_ontology.json` | Entity/relationship ontology for Fabric IQ/Fabric data agent context |
| `ontology/operator_ontology.ttl` | RDF/Turtle version of the same ontology |
| `ontology/manual-entry.md` | Copy/paste guide for adding entity types in the Fabric Ontology UI |
| `fabric-data-agent-instructions.md` | Instructions and example questions for a Fabric data agent |

## Recommended Fabric setup

The target workspace is now assigned to an F2 Fabric capacity and the Lakehouse has been created.

| Fabric item | Value |
|---|---|
| Capacity | `operatorfabricpoc` |
| Capacity SKU | `F2` |
| Capacity ID | `f7a57af9-0be9-4004-9f26-ef06d3c180d8` |
| Workspace | `iq-gbb-workhop` |
| Workspace ID | `71f18f4c-2832-4dbc-9fdf-178b0954cc38` |
| Lakehouse | `operator_matrix_poc` |
| Lakehouse ID | `a1d81601-1fde-43d7-98c7-8d62c077ba57` |
| SQL endpoint ID | `666986a2-6c81-4bd8-9793-0178d1d7ea5e` |
| OneLake DFS endpoint | `https://eastus2-onelake.dfs.fabric.microsoft.com` |

The CSV, instructions, ontology, and zip package were uploaded to:

```text
Files/operator_matrix_poc/
```

Delta tables were uploaded to:

```text
Tables/operator_mission_manual
Tables/simulation_assets
Tables/operator_events
Tables/call_signs
```

Next setup steps:

1. Open workspace `iq-gbb-workhop`.
2. Open Lakehouse `operator_matrix_poc`.
3. Confirm these tables appear:
   - `operator_mission_manual`
   - `simulation_assets`
   - `operator_events`
   - `call_signs`
   - `missions`
   - `mission_events`
   - `original_roles`
4. Create or update a Fabric data agent.
5. Add the Lakehouse tables and the ontology as data sources/context.
6. Paste the instructions from `fabric-data-agent-instructions.md`.
7. In Foundry, connect your `operator-manuals` agent to the Fabric data agent/Fabric IQ source.

## Native Ontology item note

The native Fabric ontology item `operator_matrix_ontology` exists in the workspace, but public Fabric REST routes for importing entity types are not currently available. The generic `updateDefinition` API returns success but resets ontology `definition.json` to `{}`. Use the Fabric portal and `ontology/manual-entry.md` to add the entity types and relationships.

## Example questions

```text
Which procedures mention checkpoint Bravo?
What is the current status of relay hardline?
Show the latest anomaly events for construct A-17.
What does call sign Sierra 1 refer to?
Which assets are connected to checkpoint Charlie?
```
