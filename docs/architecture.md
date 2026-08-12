# Architecture

```text
OpenAI client
    |
    +-- GET  /v1/models
    +-- POST /v1/chat/completions
    +-- POST /v1/responses
                  |
                  v
           OpenAI adapters
                  |
                  v
          protocol.Request
                  |
                  v
          Model Registry
      display_name -> model ID
                  |
                  v
           Qoder client
        + COSY signing
                  |
                  v
         Qoder agent SSE
                  |
                  v
         Qoder SSE parser
                  |
                  v
          protocol.Event
              /       \
             /         \
 Chat SSE/JSON     Responses SSE/JSON
```

## Package responsibilities

- `internal/credential`: local Qoder credential persistence.
- `internal/qoder/auth.go`: PKCE device login and user-info resolution.
- `internal/qoder/signer.go`: COSY AES/RSA/MD5 signing compatibility.
- `internal/qoder/models.go`: model discovery and display-name registry.
- `internal/qoder/client.go`: Qoder agent request construction.
- `internal/qoder/stream.go`: unwraps Qoder's outer SSE envelope and normalizes inner Chat-style deltas.
- `internal/protocol`: provider-neutral request/event structures.
- `internal/openai`: Chat Completions and Responses adapters.
- `internal/server`: routes and optional local API-key authentication.

## Model invariant

The model layer has one strict invariant:

```text
OpenAI-facing model id = Qoder display_name
Qoder-facing model id  = Qoder key/model_id
```

No endpoint returns the Qoder internal ID in its public model catalogue.


## Anthropic adapter

`POST /v1/messages` is implemented as a protocol adapter on top of the same Qoder backend. Anthropic text blocks normalize to the shared `protocol.Request`; `tool_use` becomes OpenAI-style `tool_calls` for Qoder, and `tool_result` becomes a `role=tool` message. Qoder OpenAI-style stream events are then encoded as Anthropic Messages SSE (`message_start`, content blocks, `message_delta`, `message_stop`). Parallel/interleaved Qoder tool-call fragments are buffered and emitted as sequential Anthropic `tool_use` blocks so each content block has a valid start/delta/stop lifecycle.

## Thinking effort mapping

Reasoning/thinking depth is normalized into `protocol.Request.ReasoningEffort` before the Qoder client is called. Public protocol fields are:

- OpenAI Chat Completions: `reasoning_effort`
- OpenAI Responses: `reasoning.effort`
- Anthropic Messages: `output_config.effort`
- Anthropic `thinking.type=disabled`: normalized to Qoder `none` when supported

Validation uses the live Qoder model entry's `thinking_config.enabled.efforts` and `thinking_config.disabled`. Unsupported levels fail at the compatibility layer with HTTP 400. Valid overrides are serialized to Qoder as `parameters.reasoningEffort`; an empty/auto/default value leaves Qoder's model/account default untouched.
