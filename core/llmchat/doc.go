// Package llmchat is the Cordum LLM Chat Assistant runtime: an in-cluster,
// informational-only Q&A service grounded in the local Cordum API and
// cordum.io knowledge pack.
//
// The chat assistant does not call MCP tools, does not submit jobs, does not
// approve or reject work, and does not mutate Cordum state. Mutations remain in
// the existing dashboard, CLI, and MCP-server paths. This package owns the chat
// session store, provider streaming, prompt loading, entitlement-gated HTTP/SSE
// and WebSocket handlers, and the chat-assistant CAP identity bootstrap with an
// empty tool scope.
//
// Knowledge-pack substituters under core/llmchat/knowledge use
// core/mcp.DefaultRedactor before inserting API/site content into model context
// as defense-in-depth against accidentally embedded credentials.
package llmchat
