---
name: scrum
description: "Scrum flow capability: absorbs raw work into an EPIC -> Story -> Task/Bug/Design pipeline with spec/plan tracking."
brief: Scrum flow capability over the unclaimed pool; read its guide before absorbing, decomposing, or staging development work.
labels: [scrum:*, scrum-stage:*, scrum-out:*]
boards: [scrum-inbox, scrum-pipeline, scrum-out-board]
---
# scrum capability — agent guide

The first stage of the flow: raw work arrives in scrum's Inbox and leaves it either claimed into the pipeline or evicted with a reason.

## Semantics

Three lanes and three owned namespaces. `scrum:*` is the claim/type axis, `scrum-stage:*` the working stage, `scrum-out:*` the evict axis.

## Actions

The verb tree lands with the recorder.

## Converge

The inbox is empty or every row carries a recorded deferral.
