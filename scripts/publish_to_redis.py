"""Publish the transformed ontology schema (build/ontology_schema.json,
produced by transform_ontology.py) to Azure Managed Redis, authenticating
with Microsoft Entra ID (no access keys, no stored secret -- the identity
running this script gets its token via GitHub Actions' OIDC federation to
Azure, configured in the workflow with azure/login).

Auth pattern per Microsoft's docs (Azure Managed Redis Entra ID auth):
  - username = Object ID of the managed identity / service principal
  - password = Entra token for scope https://redis.azure.com/.default
This script only needs a single short-lived connection to SET one key and
exit, so there's no token-refresh loop to manage (unlike a long-lived
client connection, which would need to re-AUTH before token expiry).
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import redis
from azure.identity import DefaultAzureCredential

REPO_ROOT = Path(__file__).resolve().parent.parent
SCHEMA_PATH = REPO_ROOT / "build" / "ontology_schema.json"
REDIS_KEY = "operator_matrix_ontology:schema:v1"
REDIS_SCOPE = "https://redis.azure.com/.default"


def main() -> None:
    host = os.environ["REDIS_HOST"]
    port = int(os.environ.get("REDIS_PORT", "10000"))
    object_id = os.environ["AZURE_IDENTITY_OBJECT_ID"]

    if not SCHEMA_PATH.exists():
        print(f"ERROR: {SCHEMA_PATH} not found -- run transform_ontology.py first.", file=sys.stderr)
        sys.exit(1)

    with SCHEMA_PATH.open("r", encoding="utf-8") as f:
        schema = json.load(f)

    credential = DefaultAzureCredential()
    token = credential.get_token(REDIS_SCOPE).token

    client = redis.Redis(
        host=host,
        port=port,
        ssl=True,
        username=object_id,
        password=token,
        decode_responses=True,
    )
    client.set(REDIS_KEY, json.dumps(schema))
    client.close()

    print(f"Published {REDIS_KEY} ({len(schema.get('entities', {}))} entities) to {host}:{port}")


if __name__ == "__main__":
    main()
