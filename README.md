# goboxd

goboxd is a Go HTTP service that runs submitted code through nsjail and returns per-test results. This Stage 1 build supports Python 3 as the interpreted language and C++ as the compiled language.

The server uses `net/http` because the Stage 1 API is small and the standard library keeps the service easy to build, test, and review.

## Run

```bash
make build
make test
make run
```

The service listens on `http://localhost:8080`.

## Check The Service

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/info
```

Run Python:

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "language": "py3",
    "source": "print(\"Hello, World!\")",
    "tests": [{"stdin": "", "expected_stdout": "Hello, World!"}]
  }'
```

Run C++:

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "language": "cpp",
    "source": "#include <iostream>\nint main(){std::cout<<\"hi\";}",
    "tests": [{"stdin": "", "expected_stdout": "hi"}]
  }'
```

## Stage 1 Scope

- `GET /healthz` returns `200 {"status":"ok"}`.
- `GET /readyz` checks nsjail, Python 3, and g++.
- `GET /info` returns build info, registered languages, limits, and basic stats.
- `POST /run` runs Python 3 and C++ submissions with per-test output comparison.
- Request validation covers language id, source size, test count, filenames, and C++ compiler flags.

The full API details live in [docs/api.md](docs/api.md). Security notes live in [docs/security.md](docs/security.md).

## Make Targets

```bash
make build        # build Docker image
make run          # run the service with Docker Compose
make test         # unit tests in the tools container
make integration  # integration tests against localhost:8080
make lint         # golangci-lint
make clean        # stop containers and remove volumes
```

## Configuration

- `PORT`: HTTP port, default `8080`
- `JAIL_DIR`: sandbox work directory, default `/tmp/goboxd-jail`
- `MAX_JOBS`: reported concurrency limit, default `runtime.NumCPU()`

## Notes

Stage 2 work is still pending: YAML language registration, all seven in-scope languages, and bounded queued concurrency.
