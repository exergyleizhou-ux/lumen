# Lumen Lab vs OpenClaudeScience (InternAgentS)

## Decision (2026-07)

**We ship Lumen Science Lab as its own product.**  
We do **not** chase feature-for-feature parity with [OpenClaudeScience / InternAgentS](https://github.com/qzzqzzb/OpenClaudeScience), and we do **not** replace the Go Lumen agent with LangGraph.

## What InternAgentS actually is

| | InternAgentS (OCS repo) |
|--|-------------------------|
| Agent core | **DeepAgents + LangGraph as the main path** |
| UI | **Next.js** three-panel workbench (product-polished) |
| Science files | Preview + **skills** (pdf/docx/pptx/xlsx, markitdown, 3dmol…) |
| Remote | SSH compute jobs with in-chat approval |
| Start | `./scripts/dev.sh` → UI :3000 + LangGraph :2024 + runtime :22024 |
| Not first-class | OnlyOffice Document Server, Jupyter nbconvert service, same-origin Ketcher embed |

They win on **product coherence and agent UX narrative**, not on embedding every science server.

## What Lumen Lab is

| | Lumen Science Lab |
|--|-------------------|
| Agent core | **Go Lumen agent** (SSE, permission gate, tools) |
| UI | Go-embedded static SPA (dense, evolving) |
| Science runtime | **Jupyter execute**, **same-origin Ketcher**, Research Pack / Fleet |
| Office | OnlyOffice **integration code** (view/edit/callback); DS optional / not on tiny VPS |
| LangGraph | **Optional sidecar** (`POST /api/lab/langgraph/run`) |
| Deploy | Public demo on small VPS with honest capability flags |

We win on **Lumen-native control plane + heavier science integrations + public demo**.

## Why “always building, never like them” felt true

1. **Different architecture** — cloning their *look* while keeping Go as main agent is a different product.  
2. **We optimized for capability checklists** (OO, Jupyter, Ketcher, sidecars) they largely skip as services.  
3. **They optimize for first-run product story** (UI + GIF + one launcher).  
4. **LangGraph is core there, edge here** — comparing “LangGraph panel polish” to their whole app is unfair.

## How we win from here

| Do | Don’t |
|----|--------|
| Demo script + smoke + honest health | Pretend we are InternAgentS |
| Improve Lumen main-chat UX when needed | Replace Go agent with LangGraph |
| External OnlyOffice when someone has ≥4 GiB DS | Install DS on 3.4 GiB demo VPS |
| Point people at live demo URL | Infinite parity tickets against OCS README |

## One-liner for stakeholders

> **InternAgentS** = open Claude-Science-style workbench on DeepAgents/LangGraph + Next.  
> **Lumen Lab** = Lumen’s research workbench on Go agent + science runtimes, already on our demo host.

## UI feel (2026-07)

Default chrome leans **OCS-like workbench** (files-first right pane, quiet topbar, research welcome, smoke/acceptance projects hidden in the sidebar).  
Conversation stream shows **tool cards + real approval cards** (Agent mode: write/bash ask; Bypass skips; Plan blocks writes). Sticky banner above composer while pending.  
Agent core remains **Go Lumen** — we do not switch to Next/LangGraph as the main path.
