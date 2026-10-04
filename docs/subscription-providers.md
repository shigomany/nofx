# Codex subscription and Z.ai GLM providers

This fork adds `codex` and `zai` to the existing AI provider selector. The
trading engine still owns decisions validation, sizing and order execution.
Connecting a model does not start a trader.

## OpenAI Codex

In AI configuration, choose **OpenAI Codex (subscription)** and start sign-in.
Open the official OpenAI device authorization link, enter the displayed code,
and authorize your own ChatGPT account. Then choose a model from the runtime's
catalog and run **Test connection**. A completed test verifies access to that
model; being present in the catalog alone does not verify entitlement.
The model menu prioritizes `gpt-6.1-sol`, `gpt-6-sol`, and `gpt-6-astra` when
the connected runtime exposes them. New configurations default to
`gpt-6.1-sol`; existing saved models are preserved.

Choose **Reasoning effort** before testing or saving. The setting persists
with the model and applies to trader analysis, strategy generation, and text
requests made through the selected provider. The menu shows the intersection
of the model's documented levels and the runtime's supported levels. CLI
0.159.1 exposes `low`, `medium`, `high`, `xhigh`, and `max` for these three
models. Although the API documents `none` for GPT-6 Sol, this Codex runtime
does not expose it. `ultra` is excluded because it enables automatic delegation;
the gateway is restricted to text analysis without tools or subagents.
Previously saved configurations with no effort retain `low`. Changing models
uses the new model's runtime default. Requests remain bounded by the existing
120-second generation timeout and fail without a partial trading decision.

Use **Disconnect** or remove the Codex provider to revoke its saved session.
Stop any running Codex traders before disconnecting. Closing the settings modal
only cancels a pending sign-in; it does not disconnect a saved account.

The optional private sidecar runs the pinned official `@openai/codex` App
Server. Its managed device authentication persists and renews its own session.
The browser and NOFX database receive a tenant-scoped profile reference, never
OAuth access/refresh tokens. Profiles persist in a separate Docker volume;
existing Codex credential files are not imported. There is no public sidecar
port and no mount of exchange credentials or the Docker socket. Inference is
restricted to text; local tools are disabled. Errors, timeouts and missing or
invalid final output fail the call without falling back to another provider.

To enable the extension with the repository's compose setup:

```sh
mkdir -p secrets
# Generate a fresh service credential once, without displaying it.
if [ ! -e secrets/codex-gateway-token ]; then
  (umask 077; openssl rand -hex 32 > secrets/codex-gateway-token)
fi
chmod 700 secrets
# On Linux, match the unprivileged sidecar user before starting Compose.
sudo chown 10001:10001 secrets/codex-gateway-token
sudo chmod 400 secrets/codex-gateway-token
docker compose -f docker-compose.yml -f docker-compose.providers.yml up -d --build
```

Do not overwrite that credential during routine updates. The ignored `secrets/`
directory is mounted as a Docker secret in the backend and sidecar only. Keep
it readable by the sidecar's UID 10001, and keep the parent directory private.
Provider configuration and application updates do not require editing the
credential. Back up application data and the separate Codex volume privately.
Do not publish these backups or authentication material.

The Codex adapter supports NOFX's text analysis and completed response callback.
Its callback emits the validated final answer once; token-level downstream
streaming and Telegram's function-tool conversation are not supported.
Subscription quotas, model access and device-auth eligibility depend on the
connected account. No live trading test is performed by deployment.

## Z.ai GLM API

Choose **Z.ai GLM (API)**, enter a standard Z.ai API key, and select a model.
The default is `glm-5.3` at `https://api.z.ai/api/paas/v4`; keys use the existing
encrypted NOFX configuration storage. GLM thinking is disabled by default to
keep the existing completion token budget usable. Standard tool and streaming
requests retain the existing OpenAI-compatible behavior.

**This provider uses separately billed standard API access.** Z.ai Coding Plan
benefits are restricted to its officially supported tools/products; NOFX is
not one of them. This integration does not use or impersonate a supported
coding client, and the Coding Plan endpoint is rejected. A Coding Plan
subscription or its key alone does not establish access to the standard API.

Official references:

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)
- [GPT-6.1 Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol)
- [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol)
- [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra)
- [Codex authentication](https://learn.chatgpt.com/docs/auth)
- [Z.ai OpenAI-compatible API](https://docs.z.ai/guides/develop/openai/python)
- [Z.ai Coding Plan usage policy](https://docs.z.ai/devpack/usage-policy)
