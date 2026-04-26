---
sidebar_position: 1
title: Architecture
---

# Cordum Architecture

Cordum is a safety-first agent orchestration platform built on Go, with seven services coordinating job submission, policy evaluation, and audit.

## Components

- **API Gateway** — HTTP/gRPC entry point.
- **Scheduler** — work assignment and dispatch.
- **Safety Kernel** — policy evaluation.

<Tabs>
  <TabItem value="quick">Quick start</TabItem>
  <TabItem value="prod">Production setup</TabItem>
</Tabs>

## Data Plane

NATS handles bus messages; Redis holds state.
