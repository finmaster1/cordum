package audit

// SIEMEvent.Action canonical chat-lifecycle action strings emitted by
// core/llmchat. Centralizing them here keeps the chat bootstrap path and
// phase-5 websocket/session handlers on the same wire-format values.
const (
	SIEMActionChatSessionStarted      = "chat.session_started"
	SIEMActionChatSessionClosed       = "chat.session_closed"
	SIEMActionChatBootstrapRegistered = "chat.bootstrap_registered"
)
