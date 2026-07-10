# Build — runs on the builder's native arch, cross-compiles to the target arch
# (TARGETOS/TARGETARCH are provided by buildx), so no QEMU emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH

WORKDIR /app
COPY . .

# Three binaries in one image: the manager (HTTP + Postgres + messaging hub), signup, and auditor.
# Same module, same layer cache; each runtime picks one via its entrypoint/command.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o signup  ./cmd/signup && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o auditor ./cmd/auditor

# Run
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /app/manager /manager
COPY --from=build /app/signup  /signup
COPY --from=build /app/auditor /auditor
USER 65532:65532
EXPOSE 8080
# Default = the manager. The signup/auditor workloads override this with command: ["/signup"|"/auditor"].
ENTRYPOINT ["/manager"]
