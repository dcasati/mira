# Manual ontology entry guide

Fabric created the native ontology item `operator_matrix_ontology`, but the current public Fabric REST APIs do not expose entity-type import/update operations for the Ontology preview item. The generic item `updateDefinition` API returned success but reset `definition.json` to `{}`.

Use the Fabric portal to add these entity types.

## Entity types

### CallSign

Description:

```text
A radio identity used by a field unit, base unit, or monitoring unit.
```

Key field:

```text
call_sign
```

Examples:

```text
Sierra 1, Echo 2, Control
```

### Asset

Description:

```text
A fictional operational object such as a checkpoint, relay, construct, or command node.
```

Key field:

```text
asset_id
```

Examples:

```text
ASSET-002, ASSET-004
```

### Procedure

Description:

```text
A documented action sequence Operator can retrieve and summarize.
```

Key field:

```text
doc_id
```

Examples:

```text
PROC-003, PROC-004
```

### Event

Description:

```text
A time-stamped observation or operational event.
```

Key field:

```text
event_id
```

Examples:

```text
EVT-004, EVT-005
```

### Proword

Description:

```text
A radio procedure word with operational meaning.
```

Key field:

```text
proword
```

Examples:

```text
Roger, Wilco, Standby, Over, Out
```

### Mission

Description:

```text
A coordinated simulation objective that assigns call signs, targets assets, uses procedures, and produces events.
```

Key field:

```text
mission_id
```

Examples:

```text
MISSION-001, MISSION-002
```

### Role

Description:

```text
An original character-like operational role used for demo storytelling without using copyrighted character names.
```

Key field:

```text
role_id
```

Examples:

```text
ROLE-001, ROLE-002
```

## Relationships

| Relationship | From | To | Join hint |
|---|---|---|---|
| `procedure_references_asset` | Procedure | Asset | `operator_mission_manual.related_assets` contains `simulation_assets.name` |
| `event_observed_on_asset` | Event | Asset | `operator_events.asset_id = simulation_assets.asset_id` |
| `event_reported_by_call_sign` | Event | CallSign | `operator_events.call_sign = call_signs.call_sign` |
| `procedure_uses_proword` | Procedure | Proword | `operator_mission_manual.prowords` contains `Proword.proword` |
| `mission_uses_procedure` | Mission | Procedure | `missions.procedure_ids` contains `operator_mission_manual.doc_id` |
| `mission_targets_asset` | Mission | Asset | `missions.target_asset_ids` contains `simulation_assets.asset_id` |
| `mission_assigned_to_call_sign` | Mission | CallSign | `missions.assigned_call_signs` contains `call_signs.call_sign` |
| `mission_has_event` | Mission | Event | `mission_events.mission_id = missions.mission_id` and `mission_events.event_id = operator_events.event_id` |
| `role_uses_call_sign` | Role | CallSign | `original_roles.call_sign = call_signs.call_sign` |
| `role_participates_in_mission` | Role | Mission | `original_roles.related_missions` contains `missions.mission_id` |

## Synonyms

| Canonical term | Synonyms |
|---|---|
| checkpoint Bravo | Bravo, CP Bravo, B checkpoint |
| checkpoint Charlie | Charlie, CP Charlie, C checkpoint |
| relay hardline | hardline, relay link, signal relay |
| construct anomaly | anomaly, construct drift, timing drift |
| radio check | comms check, signal check, readability check |
