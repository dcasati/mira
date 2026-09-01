"""Deploy operator-router as a Foundry hosted agent.

Simplified version of ../deploy_hosted_agent.py for operator-router, which
has no Fabric/OneLake/Redis/Search dependencies (see main.py's module
docstring) -- it only needs a model deployment and the project endpoint,
so it doesn't reuse that script's persona-agent-specific required env
vars.
"""

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
agent_name = os.environ.get("FOUNDRY_HOSTED_AGENT_NAME", "operator-router")
sample_path = Path(__file__).parent.resolve()
appinsights_connection_string = os.environ.get("APPLICATIONINSIGHTS_CONNECTION_STRING", "")


def create_code_zip(source_dir: Path) -> Path:
    zip_path = Path(tempfile.gettempdir()) / f"{agent_name}.zip"
    excluded = {".git", ".venv", "__pycache__", ".env", "deploy_operator_router.py"}

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

env_vars = {
    "FOUNDRY_PROJECT_ENDPOINT": endpoint,
    "AZURE_AI_MODEL_DEPLOYMENT_NAME": model_name,
}
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
        description=(
            "Voice-Operator routing layer: classifies a spoken question and calls the "
            "matching Waypoint specialist agent (assurance-orchestrator or "
            "collaboration-evidence-expert) on mira-gateway's behalf."
        ),
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
    # created, so it's immediately reachable via its persistent endpoint.
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
            input="What can you help me with?",
        )

    print(f"Agent response: {response.output_text}")
