# MIRA — Mobile Intelligence Radio Assistant

MIRA is a headless Go voice bot for a private Zello Friends & Family channel. It runs as one AKS pod, listens for complete push-to-talk transmissions, detects the wake word locally with Whisper, keeps a 45-second Azure OpenAI Realtime conversation open, and transmits spoken responses back through Zello.

## What is implemented

- Official Zello Channel API WebSocket client for Friends & Family at `wss://zello.io/ws`
- Named-account logon with `ZELLO_AUTH_TOKEN`, `ZELLO_USERNAME`, `ZELLO_PASSWORD`, and `ZELLO_CHANNEL`
- Zello control frames and binary Opus packet framing
- WebSocket ping/pong support through the Go WebSocket stack
- reconnect backoff with jitter
- inbound stream assembly by `on_stream_start` / binary packets / `on_stream_stop`
- self-transmission filtering
- serialized half-duplex outbound transmissions
- Opus encode/decode path through libopus (`-tags opus`)
- local wake detection abstraction with whisper.cpp (`-tags whisper`)
- Azure OpenAI Realtime WebSocket client using Microsoft Entra ID via `azidentity.DefaultAzureCredential`
- persistent Azure Realtime session during the active conversation window
- explicit IDLE → ACTIVATING → ACTIVE → EXPIRING → IDLE state machine
- `/healthz`, `/readyz`, and Prometheus-style `/metrics`
- Dockerfile and AKS manifests for `namespace: mira`, workload `mira-gateway`

## Prerequisites

- Go 1.24+
- Docker
- Azure CLI
- kubectl
- an AKS cluster with outbound internet access
- an Azure OpenAI or Foundry resource with a deployed Realtime model such as `gpt-realtime`
- a dedicated Zello Friends & Family account for MIRA
- a whisper.cpp model, for example `ggml-tiny.en.bin`; the supplied Dockerfile bakes `tiny.en` into `/models/ggml-tiny.en.bin`

## Zello setup

1. Create a dedicated Zello Friends & Family account in the Zello mobile app. Do not reuse a personal account, because MIRA ignores transmissions from its configured bot username.
2. Add that user to your existing private family channel.
3. Go to <https://developers.zello.com/>, sign in, complete the developer profile, open **Keys**, and create a key.
4. For development, copy the **Sample Development Token** and use it as `ZELLO_AUTH_TOKEN`. It expires after 30 days and must not be committed or baked into the image.
5. For production, generate short-lived RS256 JWT auth tokens server-side using the developer portal **Issuer** and **Private Key**. The JWT payload uses `iss` and `exp`. Never place the private key in this repo, image, or Kubernetes Secret.

MIRA uses this official Channel API logon shape:

```json
{
  "command": "logon",
  "seq": 1,
  "auth_token": "<jwt>",
  "username": "<mira-zello-user>",
  "password": "<password>",
  "channels": ["<private-family-channel>"]
}
```

## Azure OpenAI Realtime setup

Deploy a Realtime model in a supported Azure OpenAI / Foundry region. The WebSocket endpoint used by MIRA is:

```text
wss://<resource>.openai.azure.com/openai/v1/realtime?model=<deployment-name>
```

Do not use date-based `api-version` query parameters for the GA `/openai/v1` Realtime endpoint.

MIRA requests Entra tokens with this scope:

```text
https://ai.azure.com/.default
```

Grant the AKS workload identity the least-privilege inference role:

```bash
az role assignment create \
  --assignee "$USER_ASSIGNED_CLIENT_ID" \
  --role "Cognitive Services OpenAI User" \
  --scope "$AZURE_OPENAI_RESOURCE_ID"
```

Role propagation can take several minutes.

## AKS Workload Identity

For AKS Standard clusters, enable OIDC issuer and Workload Identity:

```bash
az aks update \
  --resource-group "$AKS_RESOURCE_GROUP" \
  --name "$AKS_CLUSTER_NAME" \
  --enable-oidc-issuer \
  --enable-workload-identity
```

Create a user-assigned managed identity:

```bash
az identity create \
  --name "$IDENTITY_NAME" \
  --resource-group "$RESOURCE_GROUP" \
  --location "$LOCATION"

export USER_ASSIGNED_CLIENT_ID="$(az identity show \
  --name "$IDENTITY_NAME" \
  --resource-group "$RESOURCE_GROUP" \
  --query clientId -o tsv)"
```

Create the federated identity credential:

```bash
export AKS_OIDC_ISSUER="$(az aks show \
  --name "$AKS_CLUSTER_NAME" \
  --resource-group "$AKS_RESOURCE_GROUP" \
  --query oidcIssuerProfile.issuerUrl -o tsv)"

az identity federated-credential create \
  --name mira-gateway-fic \
  --identity-name "$IDENTITY_NAME" \
  --resource-group "$RESOURCE_GROUP" \
  --issuer "$AKS_OIDC_ISSUER" \
  --subject system:serviceaccount:mira:mira-gateway \
  --audience api://AzureADTokenExchange
```

Set the client ID in `k8s/serviceaccount.yaml`:

```yaml
azure.workload.identity/client-id: "<managed-identity-client-id>"
```

## Configuration

Production environment variables:

```text
MIRA_WAKE_WORD=MIRA
MIRA_CONVERSATION_TIMEOUT=45s
MIRA_WHISPER_MODEL_PATH=/models/ggml-tiny.en.bin
MIRA_MAX_RX_SECONDS=60
MIRA_MAX_TX_SECONDS=60
ZELLO_ENDPOINT=wss://zello.io/ws
ZELLO_USERNAME=
ZELLO_PASSWORD=
ZELLO_CHANNEL=
ZELLO_AUTH_TOKEN=
AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
AZURE_OPENAI_REALTIME_DEPLOYMENT=<deployment-name>
AZURE_OPENAI_REALTIME_VOICE=alloy
HTTP_LISTEN_ADDR=:8080
LOG_LEVEL=INFO
```

Secrets must stay outside the image and outside git. Use `k8s/secret.example.yaml.sample` only as a template. It intentionally does not use a `.yaml` extension so `kubectl apply -f k8s/` cannot overwrite the real Secret with placeholders.

## Build and test locally

Unit tests do not require real Zello, Azure, libopus, or Whisper credentials:

```bash
make tidy
make test
make build
```

Production native build requires libopus and whisper.cpp headers/libraries:

```bash
go build -tags "opus whisper" ./cmd/mira
```

Wake detector development mode accepts a mono PCM16 WAV file and does not require Zello or Azure:

```bash
MIRA_AUDIO_FILE=input.wav \
MIRA_WHISPER_MODEL_PATH=/models/ggml-tiny.en.bin \
go run -tags whisper ./cmd/mira
```

## Build and push the container

```bash
az acr build \
  --registry "$ACR_NAME" \
  --image mira:latest \
  .
```

Or build locally:

```bash
docker build -t "$ACR_NAME.azurecr.io/mira:latest" .
docker push "$ACR_NAME.azurecr.io/mira:latest"
```

## Deploy to AKS

1. Update `k8s/configmap.yaml` with your Azure OpenAI endpoint and deployment.
2. Update `k8s/serviceaccount.yaml` with the managed identity client ID.
3. Update `k8s/deployment.yaml` with your ACR image name.
4. Create a real Zello secret from the example without committing it:

```bash
kubectl create namespace mira --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic mira-zello \
  --namespace mira \
  --from-literal=ZELLO_USERNAME="$ZELLO_USERNAME" \
  --from-literal=ZELLO_PASSWORD="$ZELLO_PASSWORD" \
  --from-literal=ZELLO_CHANNEL="$ZELLO_CHANNEL" \
  --from-literal=ZELLO_AUTH_TOKEN="$ZELLO_AUTH_TOKEN"
```

5. Deploy. The default Dockerfile bakes the Whisper model into the image at `/models/ggml-tiny.en.bin`; if you change to mounted models later, update `MIRA_WHISPER_MODEL_PATH` and the Deployment volume.

```bash
kubectl apply -f k8s/
```

Check rollout, readiness, and logs:

```bash
kubectl -n mira rollout status deployment/mira-gateway
kubectl -n mira get pods
kubectl -n mira logs deploy/mira-gateway -f
kubectl -n mira port-forward deploy/mira-gateway 8080:8080
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
```

## Test from Zello

1. Open Zello on your phone with a separate family user.
2. Enter the configured private family channel.
3. Hold PTT and say: `MIRA, tell me a joke.`
4. Release PTT. MIRA should respond as one spoken Zello transmission.
5. Within 45 seconds, transmit: `Tell me another one.`
6. After 45 seconds of inactivity, transmit: `Tell me another one.` MIRA should ignore it.
7. Then transmit: `MIRA, tell me another one.` MIRA should start a new Realtime session and answer.

## Security notes

- no API keys for Azure OpenAI; use Entra ID and Workload Identity
- no Zello secrets in source, logs, image, or README
- non-root container
- no privileged mode, host networking, hostPath, or added Linux capabilities
- read-only root filesystem in Kubernetes
- TLS verification remains enabled
- only one replica is configured because multiple replicas would all join and respond in the same Zello channel

## Known limitations

- The default image bakes the `tiny.en` Whisper model, increasing image size but making pod startup deterministic. Use a mounted model if you need to swap models without rebuilding.
- The checked-in default build excludes native `opus` and `whisper` tags so CI/unit tests can run without native libraries. The Dockerfile builds the production native path.
- If an Azure Realtime WebSocket fails during an active conversation, MIRA ends that conversation and requires the wake word again rather than pretending context was preserved.
- The resampler abstraction is intentionally isolated. The default Go implementation is suitable for the proof of concept; replace it with a high-quality native SRC library if radio/audio quality requires it.
