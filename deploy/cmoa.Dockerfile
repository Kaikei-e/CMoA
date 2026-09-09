# syntax=docker/dockerfile:1
# Chat-face runtime: cmoa serve plus the git/docdag binaries a run records
# the vault against. Coding verification is out of scope; this image does
# not ship Docker and does not start model servers.
FROM golang:1.27.1-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
# Pin matches this repository's CI (.github/workflows/docdag.yml, AGENTS.md).
RUN GOBIN=/out go install github.com/Kaikei-e/DocDag/cmd/docdag@v0.4.1
COPY go.mod ./
COPY cmoa.go ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -o /out/cmoa ./cmd/cmoa

FROM debian:bookworm-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates git \
	&& rm -rf /var/lib/apt/lists/*
COPY --from=build /out/cmoa /usr/local/bin/cmoa
COPY --from=build /out/docdag /usr/local/bin/docdag
ENTRYPOINT ["/usr/local/bin/cmoa"]
CMD ["serve"]
