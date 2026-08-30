# Build a small, self-contained Go binary. The migration SQL is embedded in
# the binary, so the runtime image does not need a source tree or shell.
FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/telegram-adblock ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/telegram-adblock /telegram-adblock

USER nonroot:nonroot
ENTRYPOINT ["/telegram-adblock"]
