# Copyright (c) Microsoft. All rights reserved.
#
# Operator voice router -- AKS self-hosted variant.
#
# Identical routing logic to ../operator-router/main.py (the Foundry
# hosted-agent variant): reads the caller's question, decides which of the
# two Waypoint specialist agents (assurance-orchestrator,
# collaboration-evidence-expert) actually owns it, calls that agent's own
# Foundry Responses API endpoint, and relays the answer back.
#
# Why this variant exists: live testing (2026-09-01) measured 74-82s for
# EVERY call to the Foundry-hosted operator-router, including calls within
# the same warm conversation (previous_response_id chained correctly, only
# ~5-10s faster than a genuinely cold call) -- Foundry hosted-agent
# sandboxes have no pre-warm/min-instance option (confirmed: the SDK's
# HostedAgentDefinition exposes only cpu/memory/environment_variables/
# container_configuration/protocol_versions/code_configuration, nothing
# resembling a keep-warm or minimum-instance setting), so this tax is
# unavoidable on every single voice question if this hop stays
# Foundry-hosted. For a voice channel, that's not acceptable.
#
# This variant removes the FIRST hop's cold-start entirely (mira-gateway
# calls this pod directly in-cluster, no per-call sandbox to provision --
# same reasoning as operator-persona-agent-aks's own module docstring).
# The two downstream Waypoint agents this router still calls
# (assurance-orchestrator, collaboration-evidence-expert) remain
# Foundry-hosted -- they are the Waypoint team's own agents, not owned by
# this repo, so their own cold start is a separate, currently unaddressed
# cost outside this change's scope.
#
# azure-ai-agentserver (which agent_framework_foundry_hosting/
# ResponsesHostServer is built on) is a generic Starlette/ASGI host meant
# for "Azure AI Hosted Agent containers" broadly, not exclusively Foundry's
# managed hosting runtime -- confirmed by inspecting its run() method
# (binds 0.0.0.0:$PORT, defaulting to 8088, with no Foundry-specific
# inbound-auth requirement). So the exact same server class runs fine as a
# plain Deployment in AKS behind a ClusterIP Service, same pattern already
# proven by operator-persona-agent-aks.

import logging
import os
from typing import Annotated

import httpx
from agent_framework import Agent, tool
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.ai.agentserver.responses import InMemoryResponseProvider
from azure.core.credentials import TokenCredential
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

from workiq_client import WorkIQUnavailable, ask_workiq

load_dotenv()

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger("operator_router")

DOWNSTREAM_AGENT_SCOPE = "https://ai.azure.com/.default"

ROUTER_INSTRUCTIONS = """You are Operator's routing layer for the Waypoint invoice-assurance agents.

You are talked to over voice (via a push-to-talk radio gateway), so the human asking never says an agent's internal name -- they ask a plain question. Your only job is to pick the one downstream tool below that actually owns the question's domain, call it, and relay its answer back briefly and clearly. Do not attempt to answer domain questions yourself -- you have no knowledge of invoices, evidence, workflow status, or workplace data except what the tools return.

You have three tools:

1. ask_assurance_orchestrator
Use for questions about invoice-assurance workflow status, findings, evidence already gathered, contracts, policies, available actions, or run/case status. Also use this if the caller explicitly asks to trigger or run the assurance pipeline for a specific invoice ID, document, or case.

2. ask_collaboration_evidence_expert
Use for questions asking to find or gather NEW evidence from email, Teams, or SharePoint about a supplier invoice -- approvals, disputes, escalations, delivery or quality exceptions, or procurement/finance decisions that haven't already been surfaced.

3. ask_workiq
Use for general workplace questions that are not specific to invoice assurance -- who manages/reports to whom, meetings and calendar, general email or Teams lookups, org-chart or people questions, or anything else about Microsoft 365 workplace context. IMPORTANT: this tool answers using ONE FIXED, PRE-AUTHORIZED PERSON'S OWN mailbox/Teams/calendar/files -- not the caller's own data, and not anyone else's. Only use it for questions that fixed identity's own data could plausibly answer (e.g. org/people lookups, or that person's own meetings/email) -- do not use it as a substitute for ask_assurance_orchestrator or ask_collaboration_evidence_expert.

Routing rules:
- If a question could fit either ask_assurance_orchestrator or ask_collaboration_evidence_expert, prefer ask_assurance_orchestrator first (it already has visibility into evidence collected by the other agent) unless the caller is explicitly asking you to go find something new in email/Teams/SharePoint.
- Use ask_workiq only for general workplace/people/calendar questions clearly outside invoice assurance.
- Call exactly one tool per question unless the caller's question genuinely spans multiple domains -- in that case call each relevant tool and combine the answers.
- Never say the internal tool/agent names out loud. Speak plainly, e.g. "Checking the assurance workflow", "Looking through the evidence", or "Checking that", not "calling assurance-orchestrator".
- Keep replies brief -- one or two sentences -- since this is a voice channel. Offer to send full detail in chat if the underlying answer is long.
- If a tool call fails or returns an error, say so plainly (e.g. "That lookup didn't come back cleanly, try again") rather than inventing an answer.
"""

WORKIQ_APP_ID = "92fea0ec-dffb-41f2-9d05-f0980142edbd"  # operator-router-workiq-client
WORKIQ_TENANT_ID = "95287129-9bbb-4084-8485-dc845ae7d143"  # Caldova
WORKIQ_CACHE_PATH = os.getenv("WORKIQ_CACHE_PATH", "/workiq/cache.bin")


def _workload_identity_credential() -> TokenCredential:
    """AKS workload identity: DefaultAzureCredential automatically picks up
    the federated token via the AZURE_CLIENT_ID / AZURE_TENANT_ID /
    AZURE_FEDERATED_TOKEN_FILE env vars the azure-workload-identity webhook
    injects into this pod (see k8s/serviceaccount.yaml + the
    azure.workload.identity/use: "true" pod label). No Foundry-managed
    identity involved here -- this container is its own principal
    (id-operator-router-caldova), unlike the Foundry-hosted variant where
    Foundry hosting provisions/assigns the identity per version."""
    return DefaultAzureCredential()


def _call_downstream_agent(
    project_endpoint: str,
    agent_name: str,
    credential: TokenCredential,
    question: str,
    timeout_seconds: float = 60.0,
) -> str:
    """Calls another Foundry hosted agent's own Responses API endpoint --
    identical call shape to the Foundry-hosted variant of this router (see
    ../operator-router/main.py), and to mira-gateway's own
    internal/foundryiq/client.go Query(). Downstream agents
    (assurance-orchestrator, collaboration-evidence-expert) remain
    Foundry-hosted, so this call still pays THEIR cold-start cost -- moving
    this router to AKS only removes the FIRST hop's cold start, not
    theirs."""
    token = credential.get_token(DOWNSTREAM_AGENT_SCOPE).token
    url = f"{project_endpoint.rstrip('/')}/agents/{agent_name}/endpoint/protocols/openai/responses?api-version=v1"

    logger.info("operator_router.downstream_call_started agent=%s", agent_name)
    response = httpx.post(
        url,
        json={"input": question},
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        timeout=timeout_seconds,
    )
    response.raise_for_status()
    data = response.json()

    if data.get("status") == "failed":
        error = data.get("error", {})
        logger.warning("operator_router.downstream_call_failed agent=%s error=%s", agent_name, error)
        raise RuntimeError(f"{agent_name} returned an error: {error.get('message', 'unknown error')}")

    answer = (data.get("output_text") or "").strip()
    if not answer:
        for item in data.get("output", []):
            if item.get("type") == "message":
                for content in item.get("content", []):
                    if content.get("text"):
                        answer = content["text"].strip()
                        break
    logger.info("operator_router.downstream_call_finished agent=%s", agent_name)
    return answer or f"{agent_name} returned no answer text."


def _build_router_tools(project_endpoint: str, credential: TokenCredential):
    @tool(
        name="ask_assurance_orchestrator",
        description=(
            "Ask the invoice-assurance workflow agent about findings, evidence already "
            "gathered, contracts, policies, available actions, or run/case status. Can "
            "also trigger the assurance pipeline for a specific invoice/case when asked."
        ),
        approval_mode="never_require",
    )
    def ask_assurance_orchestrator(
        question: Annotated[str, "The caller's question, passed through as-is."],
    ) -> str:
        return _call_downstream_agent(project_endpoint, "assurance-orchestrator", credential, question)

    @tool(
        name="ask_collaboration_evidence_expert",
        description=(
            "Ask the M365 evidence-gathering agent to find NEW evidence in email, Teams, "
            "or SharePoint about a supplier invoice -- approvals, disputes, escalations, "
            "delivery/quality exceptions, or procurement/finance decisions."
        ),
        approval_mode="never_require",
    )
    def ask_collaboration_evidence_expert(
        question: Annotated[str, "The caller's question, passed through as-is."],
    ) -> str:
        return _call_downstream_agent(project_endpoint, "collaboration-evidence-expert", credential, question)

    @tool(
        name="ask_workiq",
        description=(
            "Ask general Microsoft 365 workplace questions -- org/people lookups (who "
            "manages whom, who reports to whom), meetings and calendar, or general "
            "email/Teams context. Answers using one fixed, pre-authorized identity's own "
            "mailbox/Teams/calendar/files, not the caller's own data."
        ),
        approval_mode="never_require",
    )
    def ask_workiq_tool(
        question: Annotated[str, "The caller's question, passed through as-is."],
    ) -> str:
        try:
            return ask_workiq(question, WORKIQ_APP_ID, WORKIQ_TENANT_ID, WORKIQ_CACHE_PATH)
        except WorkIQUnavailable as exc:
            logger.warning("operator_router.workiq_unavailable error=%s", exc)
            return f"Work IQ is not available right now: {exc}"

    return [ask_assurance_orchestrator, ask_collaboration_evidence_expert, ask_workiq_tool]


def main():
    model_name = os.getenv("AZURE_AI_MODEL_DEPLOYMENT_NAME") or os.getenv("FOUNDRY_MODEL_NAME")
    if not model_name:
        raise RuntimeError(
            "Model deployment name is not configured. Set "
            "AZURE_AI_MODEL_DEPLOYMENT_NAME or FOUNDRY_MODEL_NAME."
        )

    project_endpoint = os.environ["FOUNDRY_PROJECT_ENDPOINT"]
    credential = _workload_identity_credential()

    client = FoundryChatClient(
        project_endpoint=project_endpoint,
        model=model_name,
        credential=credential,
    )

    agent = Agent(
        client=client,
        instructions=ROUTER_INSTRUCTIONS,
        tools=_build_router_tools(project_endpoint, credential),
        default_options={"store": False},
    )

    server = ResponsesHostServer(agent, store=InMemoryResponseProvider())
    server.run()


if __name__ == "__main__":
    main()
