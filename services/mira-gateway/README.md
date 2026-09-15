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
- optional Foundry IQ manual/procedure grounding through a Foundry project agent with file search
- optional Fabric Real-Time Intelligence/Eventhouse telemetry tools for MQTT/Eventstream POC queries
- explicit IDLE → ACTIVATING → ACTIVE → EXPIRING → IDLE state machine
- `/healthz`, `/readyz`, and Prometheus-style `/metrics`
- Dockerfile and AKS manifests for `namespace: mira`, workload `mira-gateway`

## Prerequisites

- Go 1.25+ (matches `go.mod`)
- Docker
- Azure CLI
- kubectl
- an AKS cluster with outbound internet access
- an Azure OpenAI or Foundry resource with a deployed Realtime model such as `gpt-realtime`
- optional Foundry project agent with file search enabled for manual/procedure grounding
- optional Fabric Eventstream routing MQTT telemetry into a KQL database/Eventhouse table
- a dedicated Zello Friends & Family account for MIRA
- a whisper.cpp model, for example `ggml-tiny.en.bin`; the supplied Dockerfile bakes `tiny.en` into `/models/ggml-tiny.en.bin`
- only if building the full native binary locally (not required for `make build`/`make test`, see below): libopus and whisper.cpp headers/libraries on your PATH

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
MIRA_WAKE_WORD=MIRA,OPERATOR,DISPATCHER
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
MIRA_LOOKUP_FILLER=Standby.
FABRIC_KQL_ENDPOINT=https://<eventhouse-cluster>.kusto.fabric.microsoft.com
FABRIC_KQL_DATABASE=<kql-database-name>
FABRIC_KQL_TABLE=Telemetry
FABRIC_KQL_TOKEN_SCOPE=https://kusto.kusto.windows.net/.default
MIRA_TELEMETRY_TIME_COLUMN=Timestamp
MIRA_TELEMETRY_DEVICE_COLUMN=DeviceId
MIRA_TELEMETRY_DEFAULT_LOOKBACK=15m
MIRA_TELEMETRY_MAX_ROWS=20
FOUNDRY_PROJECT_ENDPOINT=https://<foundry-account>.services.ai.azure.com/api/projects/<project-name>
FOUNDRY_IQ_AGENT_NAME=operator-manuals
FOUNDRY_IQ_MODEL=gpt-5-mini
FOUNDRY_IQ_MAX_OUTPUT_CHARS=6000
HTTP_LISTEN_ADDR=:8080
LOG_LEVEL=INFO
```

The Foundry IQ settings are optional but required for the manuals POC. In Azure AI Foundry, create a project agent with file search, upload the manuals to its vector store, and set `FOUNDRY_PROJECT_ENDPOINT` plus `FOUNDRY_IQ_AGENT_NAME`. When enabled, Operator exposes `query_foundry_iq_manuals` to the Realtime model and uses it before answering manual, setup, maintenance, troubleshooting, error-code, or documented-spec questions.

`MIRA_LOOKUP_FILLER` is spoken before longer tool lookups, such as Foundry IQ manual searches or telemetry queries. Set it to a short phrase like `Hold on.` or leave it empty to disable filler speech.

The default wake words include Mira, Operator, and Dispatcher. Mira's existing
ASR spellings (Meera, Myra, Mirah, and Meara) remain supported. An explicit
`MIRA_WAKE_WORD` value replaces the default list; custom overrides are respected.

Legacy router presentation labels are parsed in the gateway before a tool result
reaches Realtime. The short radio answer becomes plain `answer` text; the full
detail is retained separately as `follow_up_context` for explicit "details" or "more"
on the same topic. A single formatted audio lookup requests speech of only that
clean short answer, without another tool call. Plain evidence, other tools,
text responses, and multi-tool reasoning retain their existing paths. Incomplete
recognized presentation envelopes produce a lookup error rather than a partial
answer or spoken formatting labels.

The legacy operator and router prompts default to one short dispatcher
transmission, aiming for 20 words or fewer: the requested fact or status, then
stop. They do not offer more detail, add unsolicited advice, or expand an
acknowledgement such as "copy" into another explanation. Explicit requests for
detail still work. Safety warnings, uncertainty, scope, and required approvals
take precedence over the length target; this is prompt guidance, not audio or
text truncation. These instructions are compiled into the gateway and packaged
in the router image, so source edits alone do not update a running deployment.

Audio response requests explicitly carry their profile's instructions, including
formatted router-answer speech requests that override session instructions.
Formatted single-answer speech uses an empty input context to avoid expanding
the answer from prior conversation history, but remains in the conversation for
follow-ups. Lookup acknowledgements use an out-of-band, empty-context response.
The gateway transmits acknowledgement audio only if its returned transcript
matches the configured phrase (ignoring case, spacing and punctuation). Missing
or mismatched transcripts are logged and the optional acknowledgement is skipped;
the lookup and its final answer continue. This does not validate the final answer's
word count or semantic content.

Example worker prompts:

```text
Operator, what does the manual say about calibrating the device?
Operator, look up the startup procedure.
Operator, what does error E42 mean?
Operator, look up the startup procedure and send it in chat.
```

The Fabric/Eventhouse settings are optional. If `FABRIC_KQL_ENDPOINT`, `FABRIC_KQL_DATABASE`, and `FABRIC_KQL_TABLE` are unset, MIRA runs exactly as a voice assistant without telemetry tools. For the POC, configure your Fabric Eventstream to accept MQTT telemetry from on-premises devices and land those events in the configured KQL table. The table must have a time column and device identifier column matching `MIRA_TELEMETRY_TIME_COLUMN` and `MIRA_TELEMETRY_DEVICE_COLUMN`.

With telemetry enabled, the Realtime model can call two tools:

- `get_device_telemetry`: recent telemetry rows for a device or across devices
- `summarize_device_metric`: count/avg/min/max/latest timestamp for one numeric metric

Operator can also call `send_zello_chat_message` during voice conversations when the worker explicitly asks to send, post, or put instructions in chat. This uses Zello `send_text_message` to post concise instructions to the current channel.

Example worker prompts:

```text
MIRA, what's the latest telemetry for pump-1?
MIRA, summarize pump-1 temperature over the last 30 minutes.
MIRA, are any devices reporting alarms right now?
```

Grant the AKS workload identity permission to query the Fabric KQL database/Eventhouse. The exact permission surface depends on your Fabric tenant setup; for the POC, reader/viewer access on the KQL database is sufficient.

Secrets must stay outside the image and outside git. Use `k8s/secret.example.yaml.sample` only as a template. It intentionally does not use a `.yaml` extension so `kubectl apply -f k8s/` cannot overwrite the real Secret with placeholders.

## Build and test locally

There are two different local build outputs — know which one you're
producing:

1. **Plain build (`make build` / `go build ./cmd/mira`, no CGO tags)** —
   this is what's checked out by default and is what running `make build`
   produces at `bin/mira`. It compiles cleanly with only the Go toolchain
   (no C dependencies), but **has no audio codec or wake-word support at
   all** — it's meant for compiling and running the Go unit test suite
   (`internal/...`), not for actually talking to Zello or detecting a wake
   word.
2. **Full native build (`-tags "opus whisper"`)** — this is the real
   production binary, requires libopus and a locally built whisper.cpp on
   your machine, and is what the Dockerfile below produces for the
   container image.

Unit tests do not require real Zello, Azure, libopus, or Whisper credentials:

```bash
make tidy
make test
make build          # -> bin/mira (plain build, no opus/whisper tags)
```

### Building the full native binary locally (macOS or Linux)

Install libopus and build whisper.cpp from the same pinned commit the
Dockerfile uses (`592feef04a18`), so your local build matches the
container image exactly:

**macOS:**
```bash
brew install opus cmake pkg-config

git clone https://github.com/ggerganov/whisper.cpp /tmp/whisper.cpp
cd /tmp/whisper.cpp && git checkout 592feef04a18
cmake -S . -B build -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF
cmake --build build --target whisper -j"$(sysctl -n hw.ncpu)"

sudo mkdir -p /usr/local/lib /usr/local/include
sudo cp -a build/bin/lib*.dylib /usr/local/lib/
sudo cp include/whisper.h /usr/local/include/
find ggml -name '*.h' -exec sudo cp {} /usr/local/include/ \;

bash models/download-ggml-model.sh tiny.en
mkdir -p ~/mira-models && cp models/ggml-tiny.en.bin ~/mira-models/
```

**Linux (Debian/Ubuntu):**
```bash
sudo apt-get install -y build-essential cmake git libopus-dev libopusfile-dev pkg-config

git clone https://github.com/ggerganov/whisper.cpp /tmp/whisper.cpp
cd /tmp/whisper.cpp && git checkout 592feef04a18
cmake -S . -B build -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF
cmake --build build --target whisper -j"$(nproc)"

sudo mkdir -p /usr/local/lib /usr/local/include
sudo cp -a build/bin/lib*.so* /usr/local/lib/
sudo cp include/whisper.h /usr/local/include/
find ggml -name '*.h' -exec sudo cp {} /usr/local/include/ \;
sudo ldconfig

bash models/download-ggml-model.sh tiny.en
mkdir -p ~/mira-models && cp models/ggml-tiny.en.bin ~/mira-models/
```

Then, from `services/mira-gateway/`:

```bash
CGO_ENABLED=1 CGO_CFLAGS="-I/usr/local/include" CGO_LDFLAGS="-L/usr/local/lib" \
  go build -tags "opus whisper" -o bin/mira ./cmd/mira
```

On macOS you may also need `install_name_tool`/`DYLD_LIBRARY_PATH=/usr/local/lib`
set when *running* the binary (not just building it), since macOS doesn't
search `/usr/local/lib` for dylibs the way Linux's `ldconfig` does:

```bash
DYLD_LIBRARY_PATH=/usr/local/lib ./bin/mira
```

Wake detector development mode accepts a mono PCM16 WAV file and does not require Zello or Azure:

```bash
MIRA_AUDIO_FILE=input.wav \
MIRA_WHISPER_MODEL_PATH=~/mira-models/ggml-tiny.en.bin \
DYLD_LIBRARY_PATH=/usr/local/lib \
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
