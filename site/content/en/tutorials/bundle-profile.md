---
title: "Bundle & Profile Layering"
description: "How the two manifests (bundle and profile) land in Go as Preset composition and layered Settings"
weight: 43
---

# Bundle & Profile Layering

## In One Sentence

**Upstream layers the final configuration from two manifests — bundle (which capabilities to pack) and profile (with which policies they run); dsh-go does not copy the manifest format but uses `pkg/presets` AgentPreset composition for capability packing and `pkg/settings` layered scopes for config overlay, with revision-based optimistic concurrency.**

## Two Manifests

| Concept | Question | Content |
|---|---|---|
| bundle | "Which capabilities do I bring?" | a packed list of tools/services/plugins |
| profile | "With which policies do they run?" | sandbox mode, approval policy, model, etc. |

The effective config is a multi-layer overlay. dsh-go maps the two layers to Preset and Settings.

## Preset: Declare & Compose Capabilities

`pkg/presets.AgentPreset` declares which tools a standing enables and its permission/sandbox policy. `PresetRegistry` mounts and selects; `ComposeFrom` overlays bases into a new preset:

```go
web := presets.ComposeFrom("web", base, extra)
reg := presets.NewPresetRegistry()
reg.Mount(web)
p, ok := reg.Select("web")
```

On the permission side, `PermissionPreset` and `Derive` take a preset name plus runtime sandbox/approval overrides and produce the final `DerivedState`, with overrides winning over defaults.

## Settings: Layered Overlay & Path Access

`pkg/settings` models config as a tree addressed by dotted `Path` (e.g. `llm.temperature`); `SettingsScope` holds layered scopes:

- **ApplyHost**: write host defaults;
- **Update / Replace**: incremental set/unset or full replace;
- **Get / Merge**: reads merge layers, nearer wins;
- **Describe**: export the full tree, redacting paths marked via `MarkSecret`.

### Revision Optimistic Concurrency

`SettingsScope` uses a monotonic `Revision`: every Update/Replace carries the expected revision; a mismatch returns `ErrRevisionMismatch` (`IsRevisionMismatch`), and the caller re-reads and retries — preventing silent read-modify-write overwrites.

## Secret Safety

`MarkSecret` marks a path as a key so that `Describe(redactSecrets=true)` redacts it; the real secret is fetched per-request by `pkg/credentials`, rather than flowing in cleartext through the config tree.

## Trade-offs

Upstream manifests are declarative files for distribution; dsh-go as a library uses typed Go structs for compile-time field checks, at the cost of configs not existing as standalone files. The core semantics — layered overlay, nearer wins, secret redaction — are fully retained.

## Source Map

| Concept | Go implementation | Upstream TypeScript |
|---|---|---|
| Capability preset | `pkg/presets/agent_presets.go` — `AgentPreset` | bundle manifest |
| Composition | `pkg/presets/agent_presets.go` — `ComposeFrom` | bundle overlay |
| Permission derive | `pkg/presets/permission_presets.go` — `Derive` | profile policy |
| Layered config | `pkg/settings/settings.go` — `Merge` | profile overlay |
| Optimistic concurrency | `pkg/settings/settings.go` — `Revision` | (dsh-go addition) |
| Secret redaction | `pkg/settings/settings.go` — `MarkSecret` | (dsh-go addition) |

## Next Steps

- **[Capability Seams & LLM Adapter](./capability-seams)**
- **[Plugin Kernel & Event System](./plugin-kernel)**
- **[Defensive Patterns & Postmortem](./defensive-patterns)**
