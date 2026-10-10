# Model context boundary

`modelcontext.Registry` is the only application-facing boundary for values
that are shortened before an LLM call and restored afterwards. The whole
codec lives in this one package:

| File | Role |
| --- | --- |
| `registry.go` | `Registry` facade — the only type request lifecycles use |
| `handle_table.go` | `handleTable[M]`, the generic bidirectional primitive behind every handle space |
| `sources.go` | source handles (`cN`/`dN`/`bN`/`wN`) and the tool-argument codec |
| `citations.go` | protocol prompts, `<ref/>` → `<kb/>`/`<web/>` expansion, citation stream expander |
| `model_output.go` | compact source-centric renderings of tool results |
| `resources.go` | durable-resource handles (`res://NNNN`) |
| `stream.go` | `streamHold` suffix-hold primitive and every streaming decoder |
| `tool_policy.go` | the single policy layer: per-tool key contracts + `sourceKeySpaces` dispatch |

## Identity rules

- Durable application identities: UUIDs, URLs, and
  `resource://...` references.
- Request-local model handles: `cN`, `dN`, `bN`, `wN`, and `res://NNNN`.
- Temporary handles are decoded before tool execution, persistence, or UI
  delivery. They are never accepted as durable authorization or routing data.

## Request lifecycle

1. Create one `Registry` for the complete request or Agent execution.
2. Register structured source identities as they enter the context.
3. Call `EncodeMessages` immediately before the model call.
4. Decode tool calls through `DecodeToolCalls` before parsing or execution.
5. Decode complete responses with `DecodeResponse`, or streaming text with one
   `StreamDecoder` per response channel.

Unknown handle-shaped values in declared tool fields are rejected before tool
execution. The same per-tool policy is applied when historical assistant tool
calls and tool results are replayed, so handles neither disappear nor drift to
new numbers across Agent rounds.

The registry owns codec ordering. Resource references are encoded before
source IDs so embedded identifiers in stored paths remain protected as a
complete resource reference.

## Built-in tool policies

Canonical ID-bearing arguments for knowledge context, data, and web tools
are decoded centrally through a tool-name plus JSON-field allowlist. The
`sourceKeySpaces` table is the single source of truth for which keys are
ID-bearing and which handle space they register into; per-tool `sourceIDKeys`
contracts gate which of those keys each tool may use.
`data_analysis.sql` has an explicit policy because
source handles can be embedded inside quoted SQL values; unquoted SQL aliases
and arbitrary prose are never rewritten.

Dynamic MCP tools are intentionally opaque in both arguments and results.
Their schemas and identity semantics are controlled by the MCP server, so the
application must not guess that an arbitrary `id`, `knowledge_id`, or `url`
field is durable or rewrite it. Durable resource handles inside MCP arguments
still use the normal `res://NNNN` codec. A future MCP ID mapping must be an
explicit server/tool annotation and should plug into this registry rather than
create a parallel mapper.

## Knowledge scope

Document/tag-constrained knowledge context requests are filtered by server-owned
`SearchTargets` provenance. Knowledge context results outside that subset fail closed;
only an explicit whole-KB target authorizes whole-KB content. Model handles
identify sources and never grant access to additional knowledge bases.
