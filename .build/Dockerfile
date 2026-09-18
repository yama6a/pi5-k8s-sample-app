# Build — runs on the builder's native arch, cross-compiles to the target arch
# (TARGETOS/TARGETARCH are provided by buildx), so no QEMU emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS build
ARG TARGETOS TARGETARCH

WORKDIR /app
COPY . .

# Three binaries in one image: the manager (HTTP + Postgres + messaging hub), signup, and auditor.
# Same module, same layer cache; each runtime picks one via its entrypoint/command.
# Per-arch build cache id: the amd64 and arm64 builds run concurrently and would
# otherwise thrash a single shared mount. The module cache is arch-independent.
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETARCH \
    --mount=type=cache,target=/go/pkg/mod,id=go-mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o signup  ./cmd/signup && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o auditor ./cmd/auditor

# Run
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=build /app/manager /manager
COPY --from=build /app/signup  /signup
COPY --from=build /app/auditor /auditor
USER 65532:65532
EXPOSE 8080
# Default = the manager. The signup/auditor workloads override this with command: ["/signup"|"/auditor"].
ENTRYPOINT ["/manager"]
