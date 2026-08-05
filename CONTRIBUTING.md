# Contributing

SB Heartbeat uses test-driven development for behavior changes.

1. Add or update a test that fails for the intended reason.
2. Implement the smallest safe change that makes it pass.
3. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
4. Review security invariants in `docs/product-spec.md`.
5. Keep commits focused and never add live project keys or `.env` files.

Generated SQL and GitHub workflow changes need snapshot-style assertions and a
manual review of the rendered artifact. Security changes should include hostile
inputs, not only happy-path tests.
