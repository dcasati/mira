# Copyright (c) Microsoft. All rights reserved.
#
# Microsoft Teams Shifts lookup for operator-router.
#
# Why this exists (not just another Work IQ question): Work IQ's "ask" tool
# semantically searches documents/email/chat -- it has no connector for
# Teams Shifts schedule data, which lives in a separate structured Graph API
# (/teams/{id}/schedule/shifts), not as an indexable file. Live-tested
# 2026-09-02: asking ask_workiq "who is on shift today" came back asking the
# caller to point it at a SharePoint/Teams/file location, because there
# isn't one -- the schedule only exists inside the Shifts app. This module
# calls the Shifts API directly instead.
#
# Reuses the SAME MSAL token cache/refresh token as workiq_client.py (the
# one-time admin device-code sign-in from docs/WORKIQ_SETUP.md) -- no new
# interactive sign-in was needed. Confirmed live 2026-09-02: the existing
# cached refresh token silently acquired new Microsoft Graph scopes
# (Schedule.Read.All, User.ReadBasic.All) as soon as those scopes were added
# to the operator-router-workiq-client app registration and tenant-wide
# admin consent was granted for them -- MSAL refresh tokens aren't
# scope-restricted to what was requested at sign-in time, only to what the
# app has been granted consent for since.
#
# Same trust-model caveat as workiq_client.py: this only ever sees what the
# ONE FIXED admin identity is permitted to see (which, for Schedule.Read.All,
# is any team it's a member of or has access to) -- never anything tied to
# the actual voice caller's own identity.

import logging
import os
from datetime import datetime, timedelta
from typing import Optional
from zoneinfo import ZoneInfo

from msal import PublicClientApplication, SerializableTokenCache

logger = logging.getLogger("operator_router.shifts")

GRAPH_SCOPES = ["https://graph.microsoft.com/Schedule.Read.All", "https://graph.microsoft.com/User.ReadBasic.All"]

# "Manufacturing and Supply" team -- the only Shifts-enabled team wired up
# so far. groupId taken from the Teams deep link the user shared
# (2026-09-02): .../conversations?groupId=12cbb7b7-...&tenantId=...
DEFAULT_TEAM_ID = os.getenv("SHIFTS_TEAM_ID", "12cbb7b7-000a-47f2-b991-bff1b9188166")
DEFAULT_TEAM_NAME = os.getenv("SHIFTS_TEAM_NAME", "Manufacturing and Supply")

# Schedule's own local timezone, inferred from live shift data (2026-09-02):
# a shift shown as "8am-5pm" in the Shifts UI came back as
# 14:00-23:00Z -- that's UTC-6, i.e. America/Denver (Mountain), not the
# caller's own timezone. Shifts entries carry no explicit timezone field on
# the wire, so this has to be fixed per-team rather than derived per-call.
SCHEDULE_TIMEZONE = ZoneInfo(os.getenv("SHIFTS_TEAM_TIMEZONE", "America/Denver"))


class ShiftsUnavailable(RuntimeError):
    """Raised when Shifts data can't be reached with a silently-refreshed
    token -- e.g. the refresh token was revoked, or the fixed identity lost
    access to the team, rather than silently failing or prompting for
    interactive auth from inside an unattended pod."""


def _load_cache(cache_path: str) -> SerializableTokenCache:
    cache = SerializableTokenCache()
    if os.path.exists(cache_path):
        cache.deserialize(open(cache_path).read())
    else:
        raise ShiftsUnavailable(
            f"No Work IQ/Graph token cache found at {cache_path}. Run the one-time "
            "device-code sign-in in docs/WORKIQ_SETUP.md and mount the resulting "
            "cache as the operator-router-workiq-cache Secret."
        )
    return cache


def _get_graph_token(app_id: str, tenant_id: str, cache_path: str) -> str:
    cache = _load_cache(cache_path)
    app = PublicClientApplication(
        client_id=app_id,
        authority=f"https://login.microsoftonline.com/{tenant_id}",
        token_cache=cache,
    )
    accounts = app.get_accounts()
    if not accounts:
        raise ShiftsUnavailable("Token cache has no cached account -- redo the one-time sign-in.")

    result = app.acquire_token_silent(GRAPH_SCOPES, account=accounts[0])
    if not result or "access_token" not in result:
        error = (result or {}).get("error_description", "silent refresh returned no token")
        raise ShiftsUnavailable(f"Graph silent token refresh failed: {error}")

    return result["access_token"]


def _resolve_display_name(user_id: str, token: str, timeout_seconds: float) -> str:
    import httpx

    response = httpx.get(
        f"https://graph.microsoft.com/v1.0/users/{user_id}",
        params={"$select": "displayName"},
        headers={"Authorization": "Bearer " + token},
        timeout=timeout_seconds,
    )
    if response.status_code != 200:
        return user_id  # fall back to the raw id rather than fail the whole lookup
    return response.json().get("displayName", user_id)


def get_shift_schedule(
    date_str: Optional[str],
    app_id: str,
    tenant_id: str,
    cache_path: str,
    team_id: str = DEFAULT_TEAM_ID,
    team_name: str = DEFAULT_TEAM_NAME,
    timeout_seconds: float = 30.0,
) -> str:
    """Returns a plain-text summary of who is on shift for the given team on
    the given local calendar date (YYYY-MM-DD, or None/"today" for today in
    the schedule's own timezone). Resolves userIds to display names."""
    import httpx

    if not date_str or date_str.lower() == "today":
        target_date = datetime.now(SCHEDULE_TIMEZONE).date()
    else:
        target_date = datetime.strptime(date_str, "%Y-%m-%d").date()

    day_start_local = datetime(target_date.year, target_date.month, target_date.day, tzinfo=SCHEDULE_TIMEZONE)
    day_end_local = day_start_local + timedelta(days=1)

    token = _get_graph_token(app_id, tenant_id, cache_path)

    response = httpx.get(
        f"https://graph.microsoft.com/v1.0/teams/{team_id}/schedule/shifts",
        headers={"Authorization": "Bearer " + token},
        timeout=timeout_seconds,
    )
    if response.status_code != 200:
        raise ShiftsUnavailable(f"Shifts lookup failed ({response.status_code}): {response.text[:300]}")

    shifts = response.json().get("value", [])
    matches = []
    for shift in shifts:
        shared = shift.get("sharedShift") or shift.get("draftShift")
        if not shared or not shared.get("startDateTime"):
            continue
        start = datetime.fromisoformat(shared["startDateTime"].replace("Z", "+00:00")).astimezone(SCHEDULE_TIMEZONE)
        if day_start_local <= start < day_end_local:
            end = datetime.fromisoformat(shared["endDateTime"].replace("Z", "+00:00")).astimezone(SCHEDULE_TIMEZONE)
            matches.append((start, end, shift["userId"]))

    if not matches:
        return f"No one is scheduled on the {team_name} shift for {target_date.isoformat()}."

    matches.sort(key=lambda m: m[0])
    lines = []
    for start, end, user_id in matches:
        name = _resolve_display_name(user_id, token, timeout_seconds)
        lines.append(f"{name}: {start.strftime('%-I:%M %p')}-{end.strftime('%-I:%M %p')}")

    logger.info("shifts.lookup_finished team=%s date=%s count=%d", team_name, target_date.isoformat(), len(matches))
    return f"{team_name} shift for {target_date.isoformat()}: " + "; ".join(lines)
