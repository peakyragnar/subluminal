# Deep Knowledge Tutorial: Subluminal

**Purpose**: Master every concept in Subluminal from first principles. Each section builds on the previous.

---

## Part 1: Foundational Concepts

### 1.1 Processes and Standard I/O

### 1.2 Inter-Process Communication (IPC)

### 1.3 What is a Proxy?

### 1.4 What is a Shim?

### 1.5 JSON-RPC Protocol Basics

---

## Part 2: The Agent Tool Execution Problem

### 2.1 What AI Agents Actually Do

### 2.2 The Tool Call Boundary

### 2.3 The Three Gaps: Visibility, Control, Auditability

### 2.4 Why Existing Solutions Fail

---

## Part 3: Subluminal's Core Mental Model

### 3.1 Semantic Firewall Concept

### 3.2 DVR (Record Everything) Concept

### 3.3 Unix Philosophy: Primitives Not Platforms

---

## Part 4: Architecture

### 4.1 Data Plane vs Control Plane

### 4.2 Why This Split Matters

### 4.3 Shim-Per-Server Design

### 4.4 The Full Architecture Diagram

---

## Part 5: MCP Protocol

### 5.1 What is MCP?

### 5.2 Tools Discovery (tools/list)

### 5.3 Tool Invocation (tools/call)

### 5.4 MCP Transports: stdio vs HTTP

### 5.5 Where Subluminal Sits in the Stack

---

## Part 6: The Shim (Data Plane Core)

### 6.1 Shim Responsibilities

### 6.2 Shim Lifecycle

### 6.3 Message Interception

### 6.4 Policy Evaluation Point

### 6.5 Event Emission

---

## Part 7: Events (The Append-Only Truth)

### 7.1 Why Events?

### 7.2 JSONL Format

### 7.3 Event Types and Sequence

### 7.4 Common Envelope Fields

### 7.5 Event-Specific Fields

---

## Part 8: Policy System

### 8.1 What is a Policy?

### 8.2 Policy Modes: Observe, Guardrails, Control

### 8.3 Rule Kinds

### 8.4 Rule Matching

### 8.5 Rule Evaluation Order

### 8.6 Policy Compilation and Snapshots

---

## Part 9: Stateful Enforcement

### 9.1 Budgets

### 9.2 Rate Limits (Token Bucket)

### 9.3 Circuit Breakers

### 9.4 Dedupe Windows

### 9.5 Local State, No External Dependencies

---

## Part 10: Decisions and Errors

### 10.1 Decision Types

### 10.2 JSON-RPC Error Shape

### 10.3 Error Codes

### 10.4 Deterministic Errors (No Hanging)

---

## Part 11: REJECT_WITH_HINT (The Moat)

### 11.1 Why This Matters

### 11.2 Hint Structure

### 11.3 Hint Kinds

### 11.4 Suggested Args

### 11.5 Promote Hint to Rule

---

## Part 12: Secret Injection

### 12.1 The Problem: Agents Shouldn't See Secrets

### 12.2 Spawn-Time Injection

### 12.3 Secret Bindings Configuration

### 12.4 Redaction Rules

---

## Part 13: Process Supervision

### 13.1 Why No Zombies Matters

### 13.2 Signal Propagation

### 13.3 EOF Handling

### 13.4 Process Groups

---

## Part 14: Bounded Inspection

### 14.1 The Large Payload Problem

### 14.2 Always Forward, Never Block

### 14.3 Truncation Thresholds

### 14.4 Rolling Hashes

---

## Part 15: Canonicalization and Hashing

### 15.1 Why Consistent Hashing Matters

### 15.2 Canonical JSON Rules

### 15.3 args_hash Computation

### 15.4 Shared Library Requirement

---

## Part 16: Identity

### 16.1 Identity Envelope Fields

### 16.2 Workload Context

### 16.3 How Identity is Established

### 16.4 Policy Targeting with Identity

---

## Part 17: The Ledger

### 17.1 Purpose

### 17.2 SQLite and WAL Mode

### 17.3 Schema

### 17.4 Indexes

### 17.5 Event Ingestion

### 17.6 Backpressure Strategy

---

## Part 18: The CLI

### 18.1 Command Structure

### 18.2 The Import Flow

### 18.3 sub run

### 18.4 sub tail and sub query

---

## Part 19: Deployment Profiles

### 19.1 Desktop (Dev Loop)

### 19.2 CI/Container (Gatekeeper)

### 19.3 Kubernetes Sidecar (Fleet)

---

## Part 20: Contract Tests

### 20.1 Why Contract Tests?

### 20.2 Test Categories

### 20.3 P0 vs P1

### 20.4 Golden Fixtures

---

## Part 21: Implementation Tracks

### 21.1 Track Overview

### 21.2 Dependencies

### 21.3 Parallelization Strategy

---

## Part 22: Milestones

### 22.1 M0: Contracts

### 22.2 M1: v0.1 Desktop Observe

### 22.3 M1.5: Headless CI

### 22.4 M2: Guardrails

### 22.5 M3: Control

---

## Summary: The 10 Things You Must Know Cold

1. Data plane vs control plane split
2. Shim-per-server architecture
3. Event stream is the source of truth
4. Policy is compiled to snapshots
5. Decisions are deterministic JSON-RPC errors
6. REJECT_WITH_HINT enables agent recovery
7. Secrets are spawn-time injected
8. No zombies (signal propagation is P0)
9. Bounded inspection (always forward)
10. Contract tests define correct
