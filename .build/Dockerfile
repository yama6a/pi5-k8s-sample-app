# Build
FROM golang:1.26-alpine AS build

WORKDIR /app
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags='-s -w' -o app ./cmd/app

# Run
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /app/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]
