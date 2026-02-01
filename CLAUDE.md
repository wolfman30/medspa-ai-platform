# Claude Code Instructions

> Claude-specific workflow instructions for this project.
> For business requirements and technical specs, see **[SPEC.md](SPEC.md)**.

---

## Quick Reference

**What we're building:** SMS AI receptionist for medical spas (see SPEC.md for details)

**The 4-step process:**
1. Missed call → instant text back (<5 sec)
2. AI qualifies lead (service, timing, patient type)
3. Collect deposit via Square (per-clinic config)
4. Confirm to patient and operator

**Definition of Done:** All acceptance tests pass
```bash
go test -v ./tests/...
```

---

## Autonomous Operation

When working autonomously:
1. Run `go test -v ./tests/...` to check current status
2. Fix any failing tests first
3. Implement features aligned with the 4-step process (see SPEC.md)
4. Write tests for new code
5. Run full test suite before committing
6. Keep solutions simple—avoid over-engineering

---

## Common Commands

```bash
# Development
docker compose up -d              # Start services
make run-api                      # Start API server

# Testing
make test                         # Unit tests
go test -v ./tests/...            # Acceptance tests (stop condition)
make cover                        # With coverage

# Code quality
gofmt -s -w .                     # Format
go vet ./...                      # Lint

# Build
go build -v ./...                 # Build all
```

---

## Code Standards

- Read SPEC.md before making changes
- Table-driven tests with `testify/assert`
- Error wrapping: `fmt.Errorf("context: %w", err)`
- No over-engineering—only build what's in the spec
- Run tests after each significant change
