---
name: connector-required
description: Conventions for Patio web UI (web/src) pages that need an AI connector to do anything, such as Chats and Scheduled tasks. Use when building or changing any AI feature page, form, or empty state that picks a connector (chat, scheduled task, agent, automation, prompt runner), so users with no connectors get pointed to the Connectors page instead of an empty picker.
---

# Connector required

Every AI feature runs through a connector (Claude Code, Codex, Copilot, opencode, or Local AI). Users with zero connectors can't do anything on these pages, so the page should say why and give them one click to fix it, instead of showing an empty select or a disabled button with no explanation.

Reference: `ConnectorRequired` in `web/src/Connectors.tsx`, used by `web/src/Chats.tsx` and `web/src/ScheduledTasks.tsx`.

## The component

```tsx
import { ConnectorRequired } from "./Connectors";

<ConnectorRequired then="create a scheduled task" />
```

It renders a complete `.panel skill-empty` card: the `connector` icon mark, `<h2>No connectors yet</h2>`, the shared sentence "Add a connector first (Claude Code, Codex, Copilot, opencode, or Local AI), then <then>.", and a **Go to Connectors** button that navigates to `/ai/connectors`. Pass `then` as a lowercase verb phrase for what the page does. Don't wrap it in another panel, and don't copy the card, sentence, or button into a page; if the look or wording changes, it should change in one place.

## Where it goes

Load connectors with `api.get<Connector[]>("/api/connectors")` and keep the state `undefined` until loaded, then check `connectors?.length === 0`. This avoids flashing the message before the request returns.

- **List page with no connectors**: show `ConnectorRequired` in place of the normal empty state. Hide the page's primary "New <thing>" buttons, since creating one would fail anyway. Existing items still list below.
- **Main empty area** (like Chats with no chat open): show `ConnectorRequired` in place of the whole empty-area section.
- **Create form with no connectors**: show only `ConnectorRequired` and skip the form.
- **Edit form for an existing item whose connector is gone**: keep the form so the user can still read, disable, or delete the item, show `ConnectorRequired` above it, and keep Save disabled until a connector is picked.

## Checklist

- No new copy of the sentence; use the component.
- Message appears only after connectors finish loading.
- The Go to Connectors button is a real `<button>` reachable by keyboard.
- Plain `var(--border)` panels, no colored accent stripes (AGENTS.md).
- Verify with `pnpm --dir web build`.
