---
sidebar_position: 2
title: Audit
---

# Audit chain

Every policy decision and tool invocation enters the audit chain. The chain is hash-linked; tampering propagates forward.

<Admonition type="note">
Use `/api/v1/audit/verify` to validate chain integrity.
</Admonition>

## Retention

Default retention is 90 days; Enterprise tier has unlimited retention.
