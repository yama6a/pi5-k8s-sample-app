# Builds on the native arch and cross-compiles to TARGETARCH, so buildx needs no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
ARG TARGETOS TARGETARCH

WORKDIR /app
COPY . .

# One build cache per arch: the amd64 and arm64 builds run at once and would thrash a shared one.
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETARCH \
    --mount=type=cache,target=/go/pkg/mod,id=go-mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o signup  ./cmd/signup && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o auditor ./cmd/auditor

FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /
COPY --from=build /app/manager /manager
COPY --from=build /app/signup  /signup
COPY --from=build /app/auditor /auditor
USER 65532:65532
EXPOSE 8080
# The signup and auditor workloads override this with their own command.
ENTRYPOINT ["/manager"]
