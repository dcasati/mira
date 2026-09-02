# Work IQ setup for operator-router

**One-time setup, completed 2026-09-02, in the Caldova tenant
(`95287129-9bbb-4084-8485-dc845ae7d143`).** This document lets it be
reproduced/redone (e.g. if the refresh token is revoked, or this needs to
run in a fresh tenant) without repeating the research.

## Why this exists

Work IQ (Microsoft's workplace-intelligence API — org/people, email,
Teams, calendar, files) has **no application-only auth mode**, by design,
confirmed directly from Microsoft's own docs:

> "Work IQ uses Microsoft Entra ID delegated authentication. Requests run
> in the context of the signed-in user... **Application-only
> authentication isn't supported**."
> — https://learn.microsoft.com/microsoft-365/copilot/extensibility/work-iq/api-overview

So `operator-router` (a fully unattended voice pipeline, no signed-in
caller, ever) can't reach Work IQ the way an interactive Copilot session
can. The workaround: **pre-authorize one fixed, real Microsoft 365 identity
ONCE**, then refresh that identity's token silently forever after — no
interactive prompt on any subsequent call. This works because the
`WorkIQAgent.Ask` OAuth scope grants a refresh token (via the standard
`offline_access` implication), and MSAL's `acquire_token_silent` uses that
refresh token without ever needing a browser/device-code prompt again.

**Important limitation, by design**: this only ever answers using the
**one fixed identity's own mailbox/Teams/calendar/files** — never the
voice caller's own data (there's no caller-identity concept in a Zello
radio channel to delegate from in the first place).

## What was chosen

- **Identity**: `admin@caldova63232896.onmicrosoft.com` (Global
  Administrator in this tenant). Chosen because it already had a working
  Microsoft 365 Copilot / Work IQ session (confirmed live via Copilot
  Chat), and the tenant's E7 license pool was fully consumed (53/53 seats)
  with no spare seat to create a dedicated service user.
- **Trust model note**: this is the only identity in the whole Mira/Waypoint
  pipeline that's a genuine delegated-user credential — every other
  identity (mira-gateway, operator-router's own AKS workload identity,
  the Foundry agents) is a pure app-only/managed identity with no human
  in the loop, ever. Treat this one differently: rotate/reauthorize it if
  the admin account's password changes or 2FA is reset, and consider
  moving to a dedicated service identity later if a spare license seat
  becomes available.

## One-time setup steps

Requires Global Administrator (for admin consent) and the identity being
authorized available to sign in interactively once.

### 1. Provision the Work IQ service principal (JIT, idempotent)

```bash
az ad sp create --id fdcc1f02-fc51-4226-8753-f668596af7f7
```

### 2. Create a single-tenant public client app registration

```bash
APP_ID=$(az ad app create \
  --display-name "operator-router-workiq-client" \
  --sign-in-audience AzureADMyOrg \
  --is-fallback-public-client true \
  --query appId -o tsv)
az ad sp create --id $APP_ID
az ad app update --id $APP_ID \
  --public-client-redirect-uris \
    "http://localhost" \
    "https://login.microsoftonline.com/common/oauth2/nativeclient" \
    "ms-appx-web://microsoft.aad.brokerplugin/$APP_ID"
```

Public client (no client secret) is sufficient — MSAL's device-code flow
and silent refresh don't need one.

### 3. Add the delegated `WorkIQAgent.Ask` permission and grant tenant-wide admin consent

```bash
az ad app permission add --id $APP_ID \
  --api fdcc1f02-fc51-4226-8753-f668596af7f7 \
  --api-permissions "0b1715fd-f4bf-4c63-b16d-5be31f9847c2=Scope"

az ad app permission admin-consent --id $APP_ID
```

**Verify** (propagation can take ~15-30s):
```bash
APP_SP_ID=$(az ad sp list --filter "appId eq '$APP_ID'" --query "[0].id" -o tsv)
az rest --method GET \
  --url "https://graph.microsoft.com/v1.0/oauth2PermissionGrants?\$filter=clientId eq '$APP_SP_ID'"
```
Expect `consentType: AllPrincipals`, `scope: WorkIQAgent.Ask`.

### 4. One-time interactive sign-in (device-code flow)

Run as the identity being authorized (`admin@caldova63232896.onmicrosoft.com`):

```python
from msal import PublicClientApplication, SerializableTokenCache

TENANT_ID = "95287129-9bbb-4084-8485-dc845ae7d143"
APP_ID = "<from step 2>"
SCOPES = ["api://workiq.svc.cloud.microsoft/WorkIQAgent.Ask"]

cache = SerializableTokenCache()
app = PublicClientApplication(client_id=APP_ID,
    authority=f"https://login.microsoftonline.com/{TENANT_ID}", token_cache=cache)
flow = app.initiate_device_flow(scopes=SCOPES)
print(flow["message"])  # visit the URL, enter the code, sign in
result = app.acquire_token_by_device_flow(flow)  # blocks until sign-in completes
open("cache.bin", "w").write(cache.serialize())
```

This produces `cache.bin` — an MSAL token cache (JSON) containing the
refresh token. **This file is a live credential — handle it like a
secret.**

### 5. Get the cache into the AKS pod

```bash
kubectl create secret generic operator-router-workiq-cache \
  -n operator-router-aks \
  --from-file=cache.bin=cache.bin
```

Mounted read-only at `/workiq/cache.bin` in the pod (see
`k8s/deployment.yaml`); `workiq_client.py`'s `get_workiq_token()` does
`acquire_token_silent` against it on every `ask_workiq` tool call — **never**
falls back to interactive/device-code from inside the pod (there's no
terminal to prompt from there; a `WorkIQUnavailable` error surfaces
instead, meaning re-do step 4 and re-create the secret).

## Values used (Caldova tenant, 2026-09-02)

| | |
|---|---|
| Tenant ID | `95287129-9bbb-4084-8485-dc845ae7d143` |
| Work IQ service principal (Microsoft's, JIT-provisioned) | `fdcc1f02-fc51-4226-8753-f668596af7f7` |
| `operator-router-workiq-client` app ID | `92fea0ec-dffb-41f2-9d05-f0980142edbd` |
| WorkIQAgent.Ask permission ID | `0b1715fd-f4bf-4c63-b16d-5be31f9847c2` |
| Pre-authorized identity | `admin@caldova63232896.onmicrosoft.com` |
| Work IQ MCP gateway | `https://workiq.svc.cloud.microsoft/mcp` |

## Verifying it still works

```bash
kubectl exec -n operator-router-aks deploy/operator-router-aks -- \
  python3 -c "from workiq_client import get_workiq_token; print(get_workiq_token('92fea0ec-dffb-41f2-9d05-f0980142edbd', '95287129-9bbb-4084-8485-dc845ae7d143', '/workiq/cache.bin')[:20])"
```
Should print the first 20 chars of a valid access token with no error.

## Source material

- Microsoft's official `iqdeepdive` sample repo (notebooks + admin setup
  doc): https://github.com/microsoft/iqdeepdive/tree/main/notebooks —
  `docs/workiq-admin-setup.md` documents this exact az CLI sequence;
  `notebooks/_shared.py` documents the same MSAL silent-refresh pattern
  used in `workiq_client.py` here.
- Work IQ API overview:
  https://learn.microsoft.com/microsoft-365/copilot/extensibility/work-iq/api-overview
