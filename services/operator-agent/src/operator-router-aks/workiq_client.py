# Copyright (c) Microsoft. All rights reserved.
#
# Unattended Work IQ access for operator-router.
#
# Work IQ's own docs are explicit that it has no application-only auth mode
# (https://learn.microsoft.com/microsoft-365/copilot/extensibility/work-iq/api-overview
# -- "Requests run in the context of the signed-in user... Application-only
# authentication isn't supported"). This module does NOT work around that --
# it uses a genuinely delegated token for one fixed, pre-authorized identity
# (admin@caldova63232896.onmicrosoft.com, chosen 2026-09-02 since it already
# had a working Microsoft 365 Copilot / Work IQ session, confirmed live, and
# no spare E7 license seat existed to create a dedicated service user).
#
# The one-time interactive step (device-code sign-in, completed 2026-09-02)
# is NOT repeated per call or per voice question. Because the app
# registration below requested WorkIQAgent.Ask with tenant-wide admin
# consent, MSAL's token cache holds a refresh token that this module
# refreshes SILENTLY on every subsequent call -- confirmed working via a
# live Work IQ MCP "ask" call using only a silently-refreshed token, no
# interactive prompt. See docs/WORKIQ_SETUP.md in this repo for the full
# one-time setup steps (service principal provisioning, app registration,
# admin consent, device-code sign-in) if this ever needs to be redone.
#
# This is a genuinely different trust model from every other identity in
# this pipeline (which are all pure app-only workload identities with no
# human in the loop, ever). Rotate/reauthorize this if the admin account's
# password changes, if the refresh token is revoked, or if delegated access
# needs to move to a dedicated service identity later (see the module
# docstring's "known limitation": this only ever sees the ONE fixed
# identity's own mailbox/Teams/files -- never the caller's).

import json
import logging
import os

from msal import PublicClientApplication, SerializableTokenCache

logger = logging.getLogger("operator_router.workiq")

WORK_IQ_GATEWAY = "https://workiq.svc.cloud.microsoft"
WORK_IQ_SCOPE = "api://workiq.svc.cloud.microsoft/WorkIQAgent.Ask"


class WorkIQUnavailable(RuntimeError):
    """Raised when Work IQ can't be reached with a silently-refreshed token
    -- e.g. the refresh token was revoked and needs a fresh interactive
    sign-in (see docs/WORKIQ_SETUP.md), rather than silently failing or
    prompting for interactive auth from inside an unattended pod."""


def _load_cache(cache_path: str) -> SerializableTokenCache:
    cache = SerializableTokenCache()
    if os.path.exists(cache_path):
        cache.deserialize(open(cache_path).read())
    else:
        raise WorkIQUnavailable(
            f"No Work IQ token cache found at {cache_path}. Run the one-time "
            "device-code sign-in in docs/WORKIQ_SETUP.md and mount the resulting "
            "cache as the operator-router-workiq-cache Secret."
        )
    return cache


def get_workiq_token(app_id: str, tenant_id: str, cache_path: str) -> str:
    """Returns a Work IQ access token via silent MSAL refresh only -- never
    falls back to an interactive prompt (there's no terminal/browser to
    prompt from inside this pod). Raises WorkIQUnavailable if silent
    refresh fails, so callers can surface a clear, actionable error instead
    of hanging or crashing on an AADSTS error."""
    cache = _load_cache(cache_path)
    app = PublicClientApplication(
        client_id=app_id,
        authority=f"https://login.microsoftonline.com/{tenant_id}",
        token_cache=cache,
    )
    accounts = app.get_accounts()
    if not accounts:
        raise WorkIQUnavailable("Work IQ token cache has no cached account -- redo the one-time sign-in.")

    result = app.acquire_token_silent([WORK_IQ_SCOPE], account=accounts[0])
    if not result or "access_token" not in result:
        error = (result or {}).get("error_description", "silent refresh returned no token")
        raise WorkIQUnavailable(f"Work IQ silent token refresh failed: {error}")

    return result["access_token"]


def ask_workiq(question: str, app_id: str, tenant_id: str, cache_path: str, timeout_seconds: float = 60.0) -> str:
    """Asks Work IQ a natural-language question via its MCP 'ask' tool,
    using the same JSON-RPC-over-SSE call shape as the iqdeepdive
    notebooks' _shared.py call_mcp helper. Returns the synthesized answer
    text."""
    import httpx

    token = get_workiq_token(app_id, tenant_id, cache_path)
    response = httpx.post(
        f"{WORK_IQ_GATEWAY}/mcp",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        },
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "ask", "arguments": {"question": question}}},
        timeout=timeout_seconds,
    )
    response.raise_for_status()
    for line in response.text.splitlines():
        if line.startswith("data:"):
            payload = json.loads(line[5:].strip())
            result = payload.get("result", {})
            for content in result.get("content", []):
                if content.get("type") == "text":
                    return content["text"]
            return json.dumps(result)
    return response.text
