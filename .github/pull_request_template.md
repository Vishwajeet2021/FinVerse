## What and why

<!-- Link the issue/requirement (FR-*/NFR-*) this implements. -->

## Docs checked/updated

<!-- Which doc(s) in docs/ did you check before implementing, and update if this
changes a schema, endpoint, event, or architectural decision?
See docs/00-master-ai-instructions.md §3-4. -->

## Tests

- [ ] Tests added/updated
- [ ] `go test ./...` (or `npm run build && npm run check`) passes locally
- [ ] Coverage maintained on domain/business-logic packages (docs/17-testing-strategy.md §2)

## Security checklist (skip if not applicable)

- [ ] No secret or credential in this diff
- [ ] New endpoints have an explicit RBAC scope + ownership check (docs/10-authentication-authorization.md §3.3)
- [ ] New PII/monetary fields have a documented data class (docs/11-security-standards.md §1)

## UI changes

<!-- If this touches the frontend, confirm you exercised it in a running browser,
not just a successful build. -->
