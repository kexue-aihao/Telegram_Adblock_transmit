# Build a small, self-contained Go binary. The migration SQL is embedded in
# the binary, so the runtime image does not need a source tree or shell.
# Pin all base images by digest. Update these pins deliberately as part of
# routine image maintenance, rather than accepting an implicit tag update.
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/telegram-adblock ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/telegram-adblock /telegram-adblock

USER nonroot:nonroot
ENTRYPOINT ["/telegram-adblock"]
