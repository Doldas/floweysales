## Summary

Describe what changed and why.

## Linked issue

Relates to #

## Validation

- [ ] `go test -race ./...` and `go vet ./...`
- [ ] `npm ci --prefix frontend && npm run build --prefix frontend`
- [ ] Tests added for changed behavior
- [ ] Updated docs and/or release notes

## Financial-data & agent safety

- [ ] Facts, assumptions, source timestamp and fiscal/calendar periods are distinguished
- [ ] No unreviewed execution of live orders
- [ ] Provider API limits, missing data and error paths considered
- [ ] No secrets in the PR
