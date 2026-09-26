# Builds on the native arch and cross-compiles to TARGETARCH, so buildx needs no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
ARG TARGETOS TARGETARCH

WORKDIR /app
COPY . .

# One build cache per arch: the amd64 and arm64 builds run at once and would thrash a shared one.
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETARCH \
    --mount=type=cache,target=/go/pkg/mod,id=go-mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o manager ./cmd/manager && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o signup  ./cmd/signup && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w' -o auditor ./cmd/auditor

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=build /app/manager /manager
COPY --from=build /app/signup  /signup
COPY --from=build /app/auditor /auditor
USER 65532:65532
EXPOSE 8080
# The signup and auditor workloads override this with their own command.
ENTRYPOINT ["/manager"]
