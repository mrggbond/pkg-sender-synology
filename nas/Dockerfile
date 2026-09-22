FROM golang:1.27.1-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go test ./... && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pkg-sender-nas ./cmd/pkg-sender-nas

FROM scratch
COPY --from=build /out/pkg-sender-nas /pkg-sender-nas

EXPOSE 9898
ENTRYPOINT ["/pkg-sender-nas"]
