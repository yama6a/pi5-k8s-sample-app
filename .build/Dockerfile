# Builds on the native arch and cross-compiles to TARGETARCH, so buildx needs no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ARG TARGETOS TARGETARCH

WORKDIR /app
COPY . .

# One build cache per arch: the amd64 and arm64 builds run at once and would thrash a shared one.
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETARCH \
    --mount=type=cache,target=/go/pkg/mod,id=go-mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o signup  ./cmd/signup && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o auditor ./cmd/auditor

FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /
COPY --from=build /app/manager /manager
COPY --from=build /app/signup  /signup
COPY --from=build /app/auditor /auditor
USER 65532:65532
EXPOSE 8080
# The signup and auditor workloads override this with their own command.
ENTRYPOINT ["/manager"]
