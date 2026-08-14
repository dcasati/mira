# Copyright (c) Microsoft. All rights reserved.
#
# Operator persona + grounding agent -- AKS self-hosted variant.
#
# This is variant #3 in a three-way hosting comparison for the same agent:
#   1. A low-code Foundry "Prompt" agent (Foundry portal, Azure-Search-
#      wrapped knowledge base only).
#   2. operator-persona-agent, a Foundry hosted agent (Azure AI Agent
#      Service), calling kb-zavatrix + a direct OneLake/Delta-table read
#      for Fabric operational data.
#   3. THIS -- the exact same agent_framework Agent (same instructions,
#      same two grounding tools, same FoundryChatClient model backend) but
#      run as a plain container in AKS instead of a Foundry-hosted-agent
#      deployment. The goal is to isolate whether Foundry's Agent Service
#      hosting/gateway layer itself adds latency, independent of the model
#      calls or the two grounding tools (which are unchanged from variant 2
#      and still reach the same endpoints the same way).
#
# Live latency comparison (2026-08-13, "Sierra 1" callsign lookup):
#   - Foundry hosted agent (variant 2), via Fabric's Data Agent MCP: ~55s
#   - AKS self-hosted (this variant), via the SAME Fabric Data Agent MCP: ~53s
#   -> hosting location made no meaningful difference; both bottlenecked on
#      the Fabric Data Agent's own NL-to-query LLM reasoning (~32s of the
#      total, confirmed via App Insights trace timing on variant 2).
#   - Direct OneLake/Delta-table read (replacing the Data Agent tool
#     entirely, see operator_matrix_data.py): ~1.4-1.7s steady-state,
#     effectively instant once warmed at startup.
# This variant now uses the same direct-read tool as operator-persona-agent
# so the earlier hosting-location comparison and the (much bigger) grounding
# tool comparison stay independent of each other.
#
# Grounding tools (identical to operator-persona-agent, see that directory's
# main.py and operator_matrix_data.py for the full rationale):
#
# 1. kb-zavatrix -- radio manuals/PDFs/field notes, via the Azure AI Search
#    knowledge-base MCP endpoint, authenticated with the Search service
#    api-key.
#
# 2. operator_matrix_lookup -- missions, assets, events, call signs,
#    checkpoints, relay hardline status -- via a DIRECT read of the
#    operator_matrix_poc Lakehouse Delta tables in OneLake, authenticated
#    with THIS container's own AKS workload identity
#    (id-operator-agent-aks), scoped to https://storage.azure.com/.default.
#    Same app-only, no-rotation pattern as operator-persona-agent -- just a
#    different identity backing it.
#
# The model backend (FoundryChatClient) is unchanged from variant 2: chat
# completions still go through the same Foundry project/model deployment.
# Only the *hosting location of the agent orchestration loop* differs, which
# is the one variable the hosting-location comparison was designed to
# isolate.
#
# azure-ai-agentserver (which agent_framework_foundry_hosting/
# ResponsesHostServer is built on) is a generic Starlette/ASGI host meant
# for "Azure AI Hosted Agent containers" broadly, not exclusively Foundry's
# managed hosting runtime -- confirmed by inspecting its run() method
# (binds 0.0.0.0:$PORT, defaulting to 8088, with no Foundry-specific
# inbound-auth requirement). So the exact same server class runs fine as a
# plain Deployment in AKS behind a ClusterIP Service.

import logging
import os
from typing import Annotated

from agent_framework import Agent, MCPStreamableHTTPTool, tool
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.ai.agentserver.responses import InMemoryResponseProvider
from azure.core.credentials import TokenCredential
from azure.identity import DefaultAzureCredential
from dotenv import load_dotenv

from operator_matrix_data import OperatorMatrixStore

# Load environment variables from .env file (local runs only; in AKS these
# come from the ConfigMap/Secret mounted via envFrom).
load_dotenv()

# Without this, operator_matrix_data's logger.info/.exception calls (e.g.
# "operator_matrix.schema_loaded source=redis|fabric ...") silently
# produce no output at all: Python's logging module has no default
# handler until one is configured, and nothing else in this process's
# dependency stack calls basicConfig() for us. This is what let a real
# outage go unobserved earlier -- worth having even just at INFO level
# for basic operational visibility into which schema source is in use.
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

# Operator's radio-dispatch persona. Kept identical to operator-persona-agent
# (variant 2) and to the constant this hosted agent's persona is based on
# (internal/azure/realtime.go SystemPrompt in the mira-gateway Go source),
# so persona/instructions are not a variable in this hosting comparison.
OPERATOR_INSTRUCTIONS = """You are Operator, the Mobile Intelligence Radio Assistant.

You communicate over a push-to-talk Zello radio channel and behave like a crisp, professional dispatch operator.

Use minimal radio procedure for spoken responses:
- Be brief. Default to one or two short sentences.
- Sound alert, precise, and controlled. Do not sound relaxed, chatty, playful, or overly friendly.
- Address the caller only when useful, for example: "wx-ops, Operator."
- Use at most one proword per reply. Do not stack prowords.
- When the caller only wakes you with "Operator" or similar, reply exactly: "Operator here."
- Never reply to a wake-only call with "Roger, go ahead, over" or any multi-proword phrase.
- Use "Standby" only before a lookup. Use "Roger" only to acknowledge. Use "Wilco" only when you will perform an action.
- Use "Over" only when you genuinely need a reply. Use "Out" only to close. Do not say "over and out."
- Do not narrate citations, source names, or internal tool names over voice.
- If the answer has multiple details, summarize the top one or two and offer to send details in chat.
- Prefer clipped operational phrasing: "Standby.", "Relay hardline had two events.", "Mission One is active.", "Details sent."

Understand NATO phonetic alphabet words and convert them when useful: Alpha A, Bravo B, Charlie C, Delta D, Echo E, Foxtrot F, Golf G, Hotel H, India I, Juliett J, Kilo K, Lima L, Mike M, November N, Oscar O, Papa P, Quebec Q, Romeo R, Sierra S, Tango T, Uniform U, Victor V, Whiskey W, X-ray X, Yankee Y, Zulu Z.

Prefer terse operational answers unless the caller requests details.
Avoid reading markdown, citations, URLs, tables, or unnecessary formatting aloud.
Never give long explanations over voice unless explicitly requested.
Your name is Operator. If asked who you are, answer as Operator, not MIRA.

You have two grounding tools:

1. kb_zavatrix_knowledge_base_retrieve
Use only for radio manuals, uploaded PDFs, field notes, radio setup, equipment configuration, troubleshooting, FT-818ND questions, G90 questions, and document/manual questions.

2. operator_matrix_lookup
Use for Fabric operational data: missions, assets, events, call signs, prowords, checkpoints, and relay hardline status. It returns the full current dataset plus the real entity/relationship schema (discovered live from Fabric's own Ontology metadata, in the tool's own description below) -- use that schema to reason across multiple linked tables for multi-hop questions (e.g. "what procedures apply to the asset that Sierra 1 last reported on" requires chaining call sign -> event -> asset -> procedure).

Routing rules:
- For ASSET IDs, missions, events, relay hardline, checkpoints, call signs, prowords, or operational status, always use operator_matrix_lookup.
- Do not answer operational data questions from kb_zavatrix_knowledge_base_retrieve.
- For radio/manual/equipment questions, use kb_zavatrix_knowledge_base_retrieve.
- If a question mixes equipment manuals and operational data, use both tools and clearly separate the answer.
- Do not tell the caller they need to retrieve something separately. You have both tools, so call them directly.
- Match informal/spoken references (e.g. "relay hardline", "checkpoint bravo") against the asset/entity names and descriptions actually returned by operator_matrix_lookup -- don't rely on memorized ID mappings, the data itself carries that information.

If information is not found, say exactly which tool was used: the manual knowledge base, the operator matrix lookup, or both."""


def _workload_identity_credential() -> TokenCredential:
    """AKS workload identity: DefaultAzureCredential automatically picks up
    the federated token via the AZURE_CLIENT_ID / AZURE_TENANT_ID /
    AZURE_FEDERATED_TOKEN_FILE env vars the azure-workload-identity webhook
    injects into this pod (see k8s/serviceaccount.yaml + the
    azure.workload.identity/use: "true" pod label). No Foundry-managed
    identity involved here -- this container is its own principal
    (id-operator-agent-aks), unlike variant 2 where Foundry hosting
    provisions/assigns the identity."""
    return DefaultAzureCredential()


def _search_api_key_header(_headers: dict[str, str]) -> dict[str, str]:
    """Inject the Azure AI Search api-key header for the kb-zavatrix MCP tool."""
    api_key = os.environ["FOUNDRY_IQ_SEARCH_API_KEY"]
    return {"api-key": api_key}


def _build_kb_zavatrix_tool(search_endpoint: str) -> MCPStreamableHTTPTool:
    return MCPStreamableHTTPTool(
        name="kb-zavatrix",
        url=f"{search_endpoint}/knowledgebases/kb-zavatrix/mcp?api-version=2026-05-01-preview",
        header_provider=_search_api_key_header,
        approval_mode="never_require",
        tool_name_prefix="kb_zavatrix",
    )


def _build_operator_matrix_tool(credential: TokenCredential):
    store = OperatorMatrixStore(
        credential=credential,
        workspace_id=os.environ["FABRIC_WORKSPACE_ID"],
        redis_host=os.getenv("REDIS_HOST"),
        redis_port=int(os.getenv("REDIS_PORT", "10000")),
    )
    # Warm the cache once at process startup: discovers the Ontology, its
    # backing Lakehouse/tables/relationships, and pre-fetches every table's
    # contents, so the first live question doesn't pay any discovery or
    # OneLake round-trip latency -- see operator_matrix_data.py's module
    # docstring for the full rationale and measured before/after numbers.
    store.warm_cache()

    # The tool's description is built from Fabric's own Ontology metadata
    # (discovered above), not written by hand -- this is what lets the
    # model reason across multiple linked tables (e.g. call sign -> event
    # -> asset -> procedure) without any hardcoded routing logic here.
    description = (
        "Look up Fabric operational data (missions, assets, events, call signs, prowords, "
        "checkpoints, relay hardline status). Returns the full current dataset. Use the "
        "entity/relationship schema below -- discovered live from Fabric's own Ontology "
        "metadata -- to chain across tables for multi-hop questions.\n\n" + store.schema_description()
    )

    @tool(name="operator_matrix_lookup", description=description, approval_mode="never_require")
    def operator_matrix_lookup(
        question: Annotated[
            str,
            "The worker's question. Used for call tracing only -- the full dataset is "
            "always returned; reason over it using the relationship schema above.",
        ],
    ) -> str:
        return store.lookup(question)

    return operator_matrix_lookup


def main():
    model_name = os.getenv("AZURE_AI_MODEL_DEPLOYMENT_NAME") or os.getenv("FOUNDRY_MODEL_NAME")
    if not model_name:
        raise RuntimeError(
            "Model deployment name is not configured. Set "
            "AZURE_AI_MODEL_DEPLOYMENT_NAME or FOUNDRY_MODEL_NAME."
        )

    search_endpoint = os.environ["FOUNDRY_IQ_SEARCH_ENDPOINT"].rstrip("/")
    credential = _workload_identity_credential()

    client = FoundryChatClient(
        project_endpoint=os.environ["FOUNDRY_PROJECT_ENDPOINT"],
        model=model_name,
        credential=credential,
    )

    agent = Agent(
        client=client,
        instructions=OPERATOR_INSTRUCTIONS,
        tools=[
            _build_kb_zavatrix_tool(search_endpoint),
            _build_operator_matrix_tool(credential),
        ],
        default_options={"store": False},
    )

    server = ResponsesHostServer(agent, store=InMemoryResponseProvider())
    server.run()


if __name__ == "__main__":
    main()
