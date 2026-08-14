# Copyright (c) Microsoft. All rights reserved.
#
# Operator persona + grounding hosted agent.
#
# Hosts Mira's "Operator" radio-dispatch persona, grounded against:
#
# 1. kb-zavatrix -- radio manuals/PDFs/field notes, via the Azure AI Search
#    knowledge-base MCP endpoint, authenticated with the Search service
#    api-key. No delegation needed here; confirmed by direct testing.
#
# 2. operator_matrix_lookup -- missions, assets, events, call signs,
#    checkpoints, relay hardline status -- via a DIRECT read of the
#    underlying operator_matrix_poc Lakehouse Delta tables in OneLake
#    (see operator_matrix_data.py), NOT through Fabric's Data Agent (in
#    either its Azure-Search-wrapped form or its native MCP endpoint).
#
# Why direct table reads instead of the Data Agent (in any form): live
# testing (App Insights traces, 2026-08-13) showed a callsign lookup
# through the Fabric Data Agent's own native MCP endpoint spent ~32 of a
# ~42s tool-call span waiting on the Data Agent itself -- confirmed to be
# the Data Agent's own internal NL-to-query LLM reasoning, not network
# transit or Fabric capacity queueing (the identical ~55-58s range
# reproduced live in Foundry's OWN playground, entirely within Foundry/
# Fabric's East US 2 infrastructure). The Data Agent's own definition
# confirmed its data source is plain "lakehouse_tables" -- a handful of
# tiny (3-6 row) Delta tables in a single Lakehouse, not a graph/ontology
# artifact requiring an LLM to interpret. Reading those tables directly
# (via OneLake's ADLS Gen2 API + the deltalake package for correct Delta
# transaction-log handling) replaces that ~32s NL-to-query hop with a
# plain in-memory substring search: measured at ~1.4-1.7s steady-state
# cold, and effectively instant once warmed (see operator_matrix_data.py's
# startup cache warm-up) -- roughly a 20-40x reduction.
#
# Auth: same app-only pattern used for the (now-removed) Fabric Data Agent
# MCP tool -- this hosted agent's own managed identity, scoped to
# https://storage.azure.com/.default (OneLake's audience). No new RBAC
# grant needed: the Member role already granted on the Fabric workspace
# for the Data Agent MCP call already covers OneLake table reads too --
# confirmed by direct testing.
#
# ks-fabriciq-ontology (the Azure-Search-wrapped ontology knowledge source
# requiring delegated auth) remains deliberately NOT wired into this
# automated path, for the reasons documented in the session history: that
# delegation requirement has no good headless/production answer. If
# ontology-style relationship queries are ever a hard requirement, they
# should stay an interactive-only capability (Foundry playground, a
# human-signed-in tool) rather than something this unattended agent calls
# itself.
#
# mira-gateway's live Zello voice loop keeps its own embedded copy of the
# persona text as a Go constant (internal/azure/realtime.go SystemPrompt)
# for its low-latency Azure OpenAI Realtime WebSocket session, and calls
# this hosted agent's Responses API for query_foundry_iq_manuals rather
# than talking to Fabric/Search directly itself.

import logging
import os
from typing import Annotated

from agent_framework import Agent, MCPStreamableHTTPTool, tool
from agent_framework.foundry import FoundryChatClient
from agent_framework_foundry_hosting import ResponsesHostServer
from azure.ai.agentserver.responses import InMemoryResponseProvider
from azure.core.credentials import TokenCredential
from azure.identity import DefaultAzureCredential, ManagedIdentityCredential
from dotenv import load_dotenv

from operator_matrix_data import OperatorMatrixStore

# Load environment variables from .env file
load_dotenv()

# Without this, operator_matrix_data's logger.info/.exception calls (e.g.
# "operator_matrix.schema_loaded source=redis|fabric ...") silently
# produce no output at all: Python's logging module has no default
# handler until one is configured. Worth having for basic operational
# visibility into which schema source (Redis vs. live Fabric fallback)
# is actually in use.
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

# Operator's radio-dispatch persona. Kept identical to the constant this
# hosted agent's persona is based on (internal/azure/realtime.go
# SystemPrompt in the mira-gateway Go source), extended with routing rules
# for the two grounding tools below.
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


def _hosted_credential() -> TokenCredential:
    """Use the platform-assigned managed identity when actually hosted by
    Foundry, otherwise fall back to DefaultAzureCredential for local runs
    (matches the pattern used by Microsoft's own reference hosted-agent
    samples, e.g. agent-foundryiq-mcp/main.py)."""
    if "FOUNDRY_HOSTING_ENVIRONMENT" in os.environ:
        return ManagedIdentityCredential()
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
    credential = _hosted_credential()

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
        # History will be managed by the hosting infrastructure, thus there
        # is no need to store history by the service. Learn more at:
        # https://developers.openai.com/api/reference/resources/responses/methods/create
        default_options={"store": False},
    )

    server = ResponsesHostServer(agent, store=InMemoryResponseProvider())
    server.run()


if __name__ == "__main__":
    main()
