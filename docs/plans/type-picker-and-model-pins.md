# Type picker and model pins

## Request

Feat-1: I want the ticket type dropdown to be a customized one with the color of the ticket type
Feat-2: ONLY IF ITS NOT TOO HARD, I wanna be able to pin a model from the models list from the supported CLI patio connectors.

## Decisions

1. Custom picker for both Type dropdowns (ticket detail sidebar, new-ticket dialog). Status/Assignee stay native.
2. `TypePicker.tsx`: trigger shows color dot + name (`.type` style); popup lists types with dots plus "No type"; arrows/Enter/Esc like `ModelPicker`.
3. Pins stored in `localStorage`, no migration.
4. Pins scoped per connector kind.
5. Hover pin icon per model row; pinned models in a "Pinned" group on top and still in their own group; "Default" (empty id) not pinnable; works everywhere `ModelPicker` is used.
6. Pinning works for all connector kinds.
7. Verify with gofmt, go vet, go test, pnpm build, wails build. Delete this plan in the last commit.

## Tasks

- [x] `TypePicker` in ticket detail and new-ticket dialog. Check: build passes; smoke test shows dots and keyboard nav.
- [ ] Model pinning in `ModelPicker`. Check: build passes; pinned model stays on top after restart.
