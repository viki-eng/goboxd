# goboxd

goboxd is a Go HTTP service that runs submitted code with nsjail and returns per-test results. This branch is prepared for Stage 1: Python 3 as the interpreted language and C++ as the compiled language.

Framework choice: `net/http`. The API surface is small, and the standard library keeps the service simple to build, test, and review.

## Team

- V8

## Supported Languages

- `py3` - Python 3
- `cpp` - C++

Language registration for this Stage 1 build is in [internal/language.go](internal/language.go#L73).

## Run Locally

Docker is the supported way to run the service.

```bash
make build
make run
```

The service listens on `http://localhost:8080`.

Equivalent Docker commands:

```bash
docker compose build goboxd
docker compose up goboxd
```

In another terminal:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/info
```

## Test

```bash
make test
```

Run integration tests after the service is already running:

```bash
make integration
```

Run lint:

```bash
make lint
```

## Example Requests

Python:

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "language": "py3",
    "source": "print(\"Hello, World!\")",
    "tests": [{"stdin": "", "expected_stdout": "Hello, World!"}]
  }'
```

C++:

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "language": "cpp",
    "source": "#include <iostream>\nint main(){std::cout<<\"hi\";}",
    "build": {
      "flags": ["-O2"]
    },
    "tests": [{"stdin": "", "expected_stdout": "hi"}]
  }'
```

## API

- `GET /healthz` returns liveness status.
- `GET /readyz` checks nsjail, Python 3, and g++.
- `GET /info` returns build info, registered languages, limits, and basic stats.
- `POST /run` executes a submission and returns build and per-test results.

More detail is in [docs/api.md](docs/api.md).

## Security Holes Closed

The Stage 1 implementation closes these reference holes:

1. Path traversal via filename: request filenames are validated in [cmd/goboxd/main.go](cmd/goboxd/main.go#L315) and [cmd/goboxd/main.go](cmd/goboxd/main.go#L325); source writes are validated again in [internal/sandbox.go](internal/sandbox.go#L245) and [internal/sandbox.go](internal/sandbox.go#L261).
2. Shell-style directory commands: jail directories use Go filesystem APIs, not shell commands, in [internal/sandbox.go](internal/sandbox.go#L220) and [internal/sandbox.go](internal/sandbox.go#L227).
3. Compiler-flag injection: C++ flags are allow-listed in [internal/language.go](internal/language.go#L118) and rejected by [internal/language.go](internal/language.go#L167), with HTTP validation in [cmd/goboxd/main.go](cmd/goboxd/main.go#L364).
4. Request size limits: HTTP body, source size, and test count are capped in [cmd/goboxd/main.go](cmd/goboxd/main.go#L275), [cmd/goboxd/main.go](cmd/goboxd/main.go#L337), and [cmd/goboxd/main.go](cmd/goboxd/main.go#L350).
5. UID/directory collisions under load: per-request directories use `os.MkdirTemp` in [internal/sandbox.go](internal/sandbox.go#L220).
6. Unbounded child output: stdout and stderr are truncated in [internal/sandbox.go](internal/sandbox.go#L236), with capture points in [internal/sandbox.go](internal/sandbox.go#L208).
7. Stale jail directories on normal execution paths: cleanup runs after each execution through [internal/executor.go](internal/executor.go#L76) and [internal/sandbox.go](internal/sandbox.go#L227).
## Security Issues Addressed

### 1. Correct Memory Enforcement

Fixed incorrect memory conversion when configuring nsjail.

Previously:

* Memory was converted from KB to MB before passing to `--rlimit_as`
* This caused extremely small limits (e.g. 1 GB became 1024 KB)

Updated:

* Pass `limits.MemoryKb` directly to nsjail

File:

* `internal/sandbox.go`

### 2. Isolated Writable Workspace

Problem:

* nsjail mounts `/` read-only
* Compiler could not create object files or executables

Solution:

* Added writable workspace mounted at `/tmp/work`
* Job directory is mounted into sandbox workspace

File:

* `internal/sandbox.go`

### 3. Explicit Environment Configuration

Problem:

* Jailed processes started with a minimal environment
* Toolchain lookup could fail

Solution:

* Added explicit PATH and TMPDIR using nsjail `--env`

File:

* `internal/sandbox.go`

### 4. Jail Escape Protection

Existing validation preserved:

* Work directory must remain inside configured jail root
* Cleanup paths validated before deletion
* Filename validation prevents traversal attacks

More detail is in [docs/security.md](docs/security.md).


## Limits

- Max source: 256 KiB
- Max tests: 50
- Max captured stdout/stderr: 1 MiB per stream
- Default port: `8080`
- Default jail directory: `/var/run/goboxd-jail`

## PR Checklist

Use this summary in the pull request:

- Team members: Vikas (`viki-eng`)
- Framework choice: `net/http`, because the API is small and the standard library is enough for routing and JSON handling.
- How to run locally: `make build`, then `make run`; use `make test` for unit tests and `make integration` after the service is running.
- Supported languages: Python 3 (`py3`) and C++ (`cpp`).
- Security holes closed: path traversal, shell-style directory commands, compiler-flag injection, request size limits, UID/directory collisions, unbounded child output, and normal-path stale jail cleanup.

## Stage Notes

This branch is Stage 1 focused. YAML language registration, all seven in-scope languages, bounded queued concurrency, and benchmark documentation are Stage 2/3 work.
