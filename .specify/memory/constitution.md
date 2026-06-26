# Project Constitution

## Core Principles

### 1. Type & Memory Safety
- **go**-specific safety practices:
    - Leverage Go's strong type system, avoid `interface{}`
  - Use `go vet` to detect suspicious constructs
  - Follow security patterns from \_Effective Go\_

### 2. Zero-Cost Abstractions
- Use interfaces and embedding instead of inheritance

### 3. TDD (Test-Driven Development)
- **Test First**: Write failing tests before implementation
- **Red-Green-Refactor**: Strictly follow TDD cycle
- **Test Naming**: `test_<function>_<scenario>_<expected_outcome>`
- **Test Framework**: Use **go-test**
- **Doc Testing**: Public APIs must have Example functions
- **Static Analysis**: Use **go-vet** for code quality

### 4. Error Handling
- Never ignore error return values
- Use `fmt.Errorf` or `errors.Wrap` to add context
- Define sentinel error variables

### 5. Performance Requirements
- Low latency, high concurrency
- Leverage goroutines and channels for concurrency
- Avoid lock contention and blocking operations on critical paths

### 6. Governance Rules
- All public APIs must have godoc comments
- Critical modules must pass `go test -race` and `go vet`
- Code reviews must verify tests precede implementation

## Language Configuration
- Programming Language: go
- Test Framework: go-test
- Documentation Framework: godoc
- Static Analysis: go-vet
- Code Comments Language: English
- Documentation Language: en
- File/Path Naming: English
