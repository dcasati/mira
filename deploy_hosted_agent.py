import os
import tempfile
import time
import zipfile
from pathlib import Path

from azure.ai.projects import AIProjectClient
from azure.ai.projects.models import (
    AgentEndpointConfig,
    CodeConfiguration,
    CodeDependencyResolution,
    FixedRatioVersionSelectionRule,
    HostedAgentDefinition,
    ProtocolConfiguration,
    ProtocolVersionRecord,
    ResponsesProtocolConfiguration,
    VersionSelector,
)
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

load_dotenv()

endpoint = os.environ["FOUNDRY_PROJECT_ENDPOINT"]
model_name = os.environ["FOUNDRY_MODEL_NAME"]
agent_name = os.environ.get("FOUNDRY_HOSTED_AGENT_NAME", "operator-persona-agent")
sample_path = Path(os.environ["FOUNDRY_SAMPLE_PATH"]).resolve()
appinsights_connection_string = os.environ.get("APPLICATIONINSIGHTS_CONNECTION_STRING", "")


def create_code_zip(source_dir: Path) -> Path:
    zip_path = Path(tempfile.gettempdir()) / f"{agent_name}.zip"
    excluded = {".git", ".venv", "__pycache__", ".env", "deploy_hosted_agent.py"}

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zip_file:
        for path in source_dir.rglob("*"):
            if not path.is_file():
                continue
            if any(part in excluded for part in path.parts):
                continue
            zip_file.write(path, path.relative_to(source_dir))

    return zip_path


def wait_for_active_version(project_client: AIProjectClient, version: str) -> None:
    for attempt in range(60):
        time.sleep(10)
        details = project_client.agents.get_version(
            agent_name=agent_name,
            agent_version=version,
        )
        status = details["status"]
        print(f"Provisioning status: {status} (attempt {attempt + 1}/60)")

        if status == "active":
            return

        if status == "failed":
            raise RuntimeError(f"Hosted agent provisioning failed: {dict(details)}")

    raise RuntimeError("Timed out waiting for the hosted agent version to become active.")


code_zip_path = create_code_zip(sample_path)

search_endpoint = os.environ["FOUNDRY_IQ_SEARCH_ENDPOINT"]
search_api_key = os.environ["FOUNDRY_IQ_SEARCH_API_KEY"]
fabric_workspace_id = os.environ["FABRIC_WORKSPACE_ID"]
redis_host = os.environ.get("REDIS_HOST", "")
redis_port = os.environ.get("REDIS_PORT", "10000")

env_vars = {
    "FOUNDRY_PROJECT_ENDPOINT": endpoint,
    "AZURE_AI_MODEL_DEPLOYMENT_NAME": model_name,
    "FOUNDRY_IQ_SEARCH_ENDPOINT": search_endpoint,
    "FOUNDRY_IQ_SEARCH_API_KEY": search_api_key,
    "FABRIC_WORKSPACE_ID": fabric_workspace_id,
}
# No FABRIC_LAKEHOUSE_ID / FABRIC_DATA_AGENT_ID / FOUNDRY_IQ_QUERY_SOURCE_TOKEN
# needed: the Lakehouse, its tables, and their relationships are all
# discovered live from Fabric's own Ontology metadata at startup (see
# operator_matrix_data.py), using this hosted agent's managed identity
# (app-only, no delegation, no rotation).
if redis_host:
    # Governed, CI-published ontology schema cache (see
    # github.com/dcasati/operator-matrix-ontology). Optional: falls back
    # to live Fabric discovery if unset or unreachable. This hosted
    # agent's own managed identity must be granted a Redis
    # access-policy-assignment (already done for operator-persona-agent).
    env_vars["REDIS_HOST"] = redis_host
    env_vars["REDIS_PORT"] = redis_port
    print(f"Ontology cache: REDIS_HOST={redis_host} will be set on the hosted agent.")
else:
    print("Ontology cache: no REDIS_HOST found in environment; will fall back to live Fabric discovery every restart.")
if appinsights_connection_string:
    env_vars["APPLICATIONINSIGHTS_CONNECTION_STRING"] = appinsights_connection_string
    print("OpenTelemetry: APPLICATIONINSIGHTS_CONNECTION_STRING will be set on the hosted agent.")
else:
    print("OpenTelemetry: no APPLICATIONINSIGHTS_CONNECTION_STRING found in environment; tracing will be disabled.")

with (
    code_zip_path.open("rb") as code_stream,
    DefaultAzureCredential() as credential,
    AIProjectClient(endpoint=endpoint, credential=credential) as project_client,
):
    created = project_client.agents.create_version_from_code(
        agent_name=agent_name,
        description="Operator radio-dispatch persona, hosted for mira-gateway to fetch instructions from.",
        definition=HostedAgentDefinition(
            cpu="0.5",
            memory="1Gi",
            code_configuration=CodeConfiguration(
                runtime="python_3_13",
                entry_point=["python", "main.py"],
                dependency_resolution=CodeDependencyResolution.REMOTE_BUILD,
            ),
            environment_variables=env_vars,
            protocol_versions=[
                ProtocolVersionRecord(protocol="responses", version="2.0.0")
            ],
        ),
        code=code_stream,
    )

    print(f"Created hosted agent version {created.version}")

    wait_for_active_version(project_client, created.version)

    # Route 100% of traffic on this agent name to the version we just
    # created, so the agent is immediately reachable via its persistent
    # endpoint (unlike the quickstart's temporary-validation pattern, we
    # keep this routing since operator-persona-agent should stay live for
    # mira-gateway to call).
    project_client.agents.update_details(
        agent_name=agent_name,
        agent_endpoint=AgentEndpointConfig(
            version_selector=VersionSelector(
                version_selection_rules=[
                    FixedRatioVersionSelectionRule(
                        agent_version=created.version,
                        traffic_percentage=100,
                    ),
                ]
            ),
            protocol_configuration=ProtocolConfiguration(
                responses=ResponsesProtocolConfiguration()
            ),
        ),
    )

    print(f"Agent endpoint configured for version {created.version}")

    agent_details = project_client.agents.get(agent_name=agent_name)
    print(f"Agent endpoint: {agent_details.agent_endpoint}")

    with project_client.get_openai_client(agent_name=agent_name) as openai_client:
        response = openai_client.responses.create(
            input="Operator, this is wx-ops.",
        )

    print(f"Agent response: {response.output_text}")
