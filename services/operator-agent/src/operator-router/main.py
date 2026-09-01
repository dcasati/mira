# Copyright (c) Microsoft. All rights reserved.
#
# Operator voice router -- a Foundry hosted agent that gives voice-Operator
# (mira-gateway) a single, stable entry point for asking any of the
# Waypoint invoice-assurance agents a question, instead of mira-gateway
# needing to know each specialist agent's name and pick between them
# itself.
#
# This agent does no domain reasoning of its own. It has one instruction:
# read the question, decide (via the model) which downstream specialist
# agent actually owns that domain, call that agent's own Responses API
# endpoint (Entra-authenticated, exactly the same call shape mira-gateway
# itself makes to a Foundry hosted agent -- see
# mira-gateway/internal/foundryiq/client.go), and relay its answer back in
# a voice-friendly form.
#
# Wired to two of the three agents live in this Foundry project
# (ai-project-waypoint-agents-rxhrdaxs) as of 2026-09-01:
#
#   - assurance-orchestrator: invoice-assurance workflow status/context
#     (findings, evidence, contracts, policies, run/case status,
#     read-only by default; can trigger the deterministic pipeline for a
#     given invoice/case when explicitly asked).
#   - collaboration-evidence-expert: M365 evidence (email/Teams/SharePoint)
#     about supplier-invoice approvals, disputes, escalations,
#     delivery/quality exceptions.
#
# invoice-analyst (the third agent in this project) is deliberately NOT
# wired in yet: its own agent definition shows an AgenticUserAuthorization
# handler with an empty client-secret/federated-client-id/scopes
# connection to its Waypoint API backend, so a plain app-only call (the
# only kind this router, or mira-gateway, can make) fails with a 500
# server_error every time -- confirmed by direct testing. Add it back here
# once that connection is actually configured with real credentials.
#
# Adding a future 4th/5th specialist agent to voice-Operator's reach is a
# change to ROUTER_INSTRUCTIONS and _build_agent_tool calls below -- NOT a
# change to mira-gateway, which only ever talks to this router by name.

import logging
import os
from typing import Annotated

import httpx
from agent_framework import Agent, tool
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.ai.agentserver.responses import InMemoryResponseProvider
from azure.core.credentials import TokenCredential
from azure.identity import DefaultAzureCredential, ManagedIdentityCredential
from dotenv import load_dotenv

load_dotenv()

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger("operator_router")

# Entra scope for calling another Foundry hosted agent's own Responses API
# endpoint -- identical to mira-gateway's own foundryiq.TokenScope (see
# internal/foundryiq/client.go), and to what this router's own instance
# identity needs "Foundry Agent Consumer" (or "Foundry User") granted for,
# at the project scope, to reach ITS downstream agents.
DOWNSTREAM_AGENT_SCOPE = "https://ai.azure.com/.default"

ROUTER_INSTRUCTIONS = """You are Operator's routing layer for the Waypoint invoice-assurance agents.

You are talked to over voice (via a push-to-talk radio gateway), so the human asking never says an agent's internal name -- they ask a plain question. Your only job is to pick the one downstream tool below that actually owns the question's domain, call it, and relay its answer back briefly and clearly. Do not attempt to answer domain questions yourself -- you have no knowledge of invoices, evidence, or workflow status except what the tools return.

You have two tools:

1. ask_assurance_orchestrator
Use for questions about invoice-assurance workflow status, findings, evidence already gathered, contracts, policies, available actions, or run/case status. Also use this if the caller explicitly asks to trigger or run the assurance pipeline for a specific invoice ID, document, or case.

2. ask_collaboration_evidence_expert
Use for questions asking to find or gather NEW evidence from email, Teams, or SharePoint about a supplier invoice -- approvals, disputes, escalations, delivery or quality exceptions, or procurement/finance decisions that haven't already been surfaced.

Routing rules:
- If a question could fit either tool, prefer ask_assurance_orchestrator first (it already has visibility into evidence collected by the other agent) unless the caller is explicitly asking you to go find something new in email/Teams/SharePoint.
- Call exactly one tool per question unless the caller's question genuinely spans both (e.g. "what's the status, and can you also check Teams for anything new") -- in that case call both and combine the answers.
- Never say the internal tool/agent names out loud. Speak plainly, e.g. "Checking the assurance workflow" or "Looking through the evidence", not "calling assurance-orchestrator".
- Keep replies brief -- one or two sentences -- since this is a voice channel. Offer to send full detail in chat if the underlying answer is long.
- If a tool call fails or returns an error, say so plainly (e.g. "That lookup didn't come back cleanly, try again") rather than inventing an answer.
"""


def _hosted_credential() -> TokenCredential:
    """Use the platform-assigned managed identity when actually hosted by
    Foundry, otherwise fall back to DefaultAzureCredential for local runs
    (same pattern as operator-persona-agent/main.py)."""
    if "FOUNDRY_HOSTING_ENVIRONMENT" in os.environ:
        return ManagedIdentityCredential()
    return DefaultAzureCredential()


def _call_downstream_agent(
    project_endpoint: str,
    agent_name: str,
    credential: TokenCredential,
    question: str,
    timeout_seconds: float = 60.0,
) -> str:
    """Calls another Foundry hosted agent's own Responses API endpoint,
    exactly the same request shape mira-gateway itself sends (see
    internal/foundryiq/client.go's Query): POST {"input": question} with a
    bearer token scoped to https://ai.azure.com/.default, no
    previous_response_id (each downstream call here is a fresh,
    independent question -- this router doesn't yet maintain its own
    multi-turn state per caller)."""
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
        # Fall back to extracting message content directly, same as
        # operator-persona-agent's client-side callers do (see
        # mira-gateway/internal/foundryiq/client.go's extractOutputText).
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

    return [ask_assurance_orchestrator, ask_collaboration_evidence_expert]


def main():
    model_name = os.getenv("AZURE_AI_MODEL_DEPLOYMENT_NAME") or os.getenv("FOUNDRY_MODEL_NAME")
    if not model_name:
        raise RuntimeError(
            "Model deployment name is not configured. Set "
            "AZURE_AI_MODEL_DEPLOYMENT_NAME or FOUNDRY_MODEL_NAME."
        )

    project_endpoint = os.environ["FOUNDRY_PROJECT_ENDPOINT"]
    credential = _hosted_credential()

    client = FoundryChatClient(
        project_endpoint=project_endpoint,
        model=model_name,
        credential=credential,
    )

    agent = Agent(
        client=client,
        instructions=ROUTER_INSTRUCTIONS,
        tools=_build_router_tools(project_endpoint, credential),
        # Same rationale as operator-persona-agent: history is managed by
        # the hosting infrastructure's own session/conversation model, not
        # by this agent re-storing it.
        default_options={"store": False},
    )

    server = ResponsesHostServer(agent, store=InMemoryResponseProvider())
    server.run()


if __name__ == "__main__":
    main()
