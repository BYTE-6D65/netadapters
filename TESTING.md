# Testing Guide

## Quick Start

```bash
# Run all tests
make test

# Check coverage
make coverage

# Generate detailed HTML report
make coverage-html
# Then open coverage.html in browser
```

## Coverage Baseline

**Established: 2025-10-15**

Starting coverage: 75.4%  
Current coverage: **82.5%**  
Target: 85%+ before new protocol implementation

### Coverage by Component

| File | Function | Coverage | Notes |
|------|----------|----------|-------|
| client.go | NewClientEmitter | 100% | ✅ |
| client.go | ID | 100% | ✅ |
| client.go | Type | 100% | ✅ |
| client.go | Emit | 87.5% | Missing: decode error early return |
| client.go | Close | 100% | ✅ |
| server.go | NewServerAdapter | 100% | ✅ |
| server.go | ID | 100% | ✅ |
| server.go | Type | 100% | ✅ |
| server.go | Start | 92.9% | Missing: server error logging path |
| server.go | Stop | 100% | ✅ |
| server.go | handleRequest | 61.8% | Missing: body read errors, publish errors, timeout paths |
| server.go | WriteResponse | 92.9% | Missing: write body error path |
| server.go | GetResponseWriter | 100% | ✅ |
| examples.go | CreateEchoResponse | 75.0% | Missing: error response creation, event creation error |
| testing.go | ParsePathParams | 91.7% | Missing: length mismatch early return |

## Test Files

- `client_test.go` - ClientEmitter unit tests (metadata, error paths)
- `server_test.go` - ServerAdapter integration and unit tests
- Tests cover:
  - Adapter/emitter lifecycle
  - Error handling (invalid payloads, missing writers, double-start)
  - Full request/response flow with engine
  - Path parameter parsing
  - Echo response generation

## Uncovered Areas

These are acceptable gaps (hard to test or rare conditions):

1. **HTTP request body read failure** - Requires mock HTTP connection failures
2. **Event publish timeout paths** - Requires bus saturation simulation
3. **HTTP server internal errors** - Rare runtime conditions
4. **Some early-return error paths** - Minor error handling branches

## Next Steps Before New Protocols

1. ✅ Establish 82.5% baseline
2. ⏳ Add integration tests for relay-node circular testing scenarios
3. ⏳ Consider adding fuzzing tests for payload handling
4. ⏳ Target 85%+ overall coverage
5. ⏳ Document protocol adapter testing patterns

## Continuous Integration

When setting up CI:

```yaml
# Example GitHub Actions snippet
- name: Run tests with coverage
  run: make coverage-report
  
- name: Check coverage threshold
  run: |
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    if (( $(echo "$COVERAGE < 80" | bc -l) )); then
      echo "Coverage $COVERAGE% is below 80% threshold"
      exit 1
    fi
```

## Milestone Checkpoint

**Date**: 2025-10-15  
**Status**: Ready for new protocol implementation  
**Prerequisites**: ✅ Complete

- ✅ Code cleanup (separated examples from production)
- ✅ Test coverage baseline established (82.5%)
- ✅ Documentation updated
- ✅ Makefile with coverage targets
- ✅ Experimental warnings added
- ✅ json/v2 integration validated (47.6 MB payload capacity)

**Next**: WebSocket, TCP, or UDP protocol adapters
