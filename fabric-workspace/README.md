# Fabric-synced workspace content

This folder is the Git integration target for the Fabric workspace containing
`operator_matrix_ontology`. Point Fabric's Git integration (Workspace settings ->
Git integration, in the Fabric portal) at this folder in this repo/branch.

Once connected and a first commit is made from Fabric, this folder will contain
the ontology's raw definition, e.g.:

```
operator_matrix_ontology.Ontology/
  definition.json
  EntityTypes/
    <id>/
      definition.json
      DataBindings/
        <id>.json
  RelationshipTypes/
    <id>/
      definition.json
  .platform
```

Do not hand-edit anything under here -- it's a Git mirror of Fabric's own item
definition and will be overwritten on the next sync. To add curated context
(synonyms, aliases) that Fabric's structural ontology doesn't capture, edit
`enrichment/synonyms.json` instead.
