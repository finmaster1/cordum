package llmchat

import (
	"context"
	"strings"

	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

type approvalPacketBus interface {
	SubscribeWithContext(subject, queue string, handler func(context.Context, *pb.BusPacket) error) error
}

type natsApprovalEventBus struct{ bus approvalPacketBus }

type noopApprovalSubscription struct{}

func NewNATSApprovalEventBus(bus approvalPacketBus) ApprovalEventBus {
	if bus == nil {
		return nil
	}
	return natsApprovalEventBus{bus: bus}
}

func (b natsApprovalEventBus) SubscribeApprovalEvents(_ context.Context, handler func(context.Context, ApprovalEvent) error) (ApprovalSubscription, error) {
	if handler == nil {
		return noopApprovalSubscription{}, nil
	}
	err := b.bus.SubscribeWithContext(ApprovalSubjectWildcard, "llm-chat-approvals", func(ctx context.Context, packet *pb.BusPacket) error {
		ev, ok := approvalEventFromPacket(packet)
		if !ok {
			return nil
		}
		return handler(ctx, ev)
	})
	return noopApprovalSubscription{}, err
}

func (noopApprovalSubscription) Unsubscribe() error { return nil }

func approvalEventFromPacket(packet *pb.BusPacket) (ApprovalEvent, bool) {
	if packet == nil || packet.GetAlert() == nil {
		return ApprovalEvent{}, false
	}
	alert := packet.GetAlert()
	if strings.TrimSpace(alert.Message) != "" {
		if ev, err := ParseApprovalEventJSON([]byte(alert.Message)); err == nil && ev.ApprovalID != "" {
			return ev, true
		}
	}
	d := alert.Details
	if len(d) == 0 {
		return ApprovalEvent{}, false
	}
	ev := ApprovalEvent{
		ApprovalID: firstNonEmpty(d["approval_id"], d["id"]),
		SessionID:  d["session_id"],
		AgentID:    d["agent_id"],
		Status:     firstNonEmpty(d["status"], d["approval_status"], d["decision"]),
		Reason:     firstNonEmpty(d["reason"], d["resolution_reason"]),
	}
	if ev.ApprovalID == "" {
		return ApprovalEvent{}, false
	}
	if strings.EqualFold(ev.Status, "approve") || strings.EqualFold(ev.Status, "approved") {
		ev.Status = ApprovalStatusResolved
	}
	if strings.EqualFold(ev.Status, "reject") || strings.EqualFold(ev.Status, "rejected") || strings.EqualFold(ev.Status, "denied") {
		ev.Status = ApprovalStatusRejected
	}
	return ev, true
}
