FROM golang:1.21 AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

WORKDIR /src
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/sub ./cmd/sub

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/sub /usr/local/bin/sub

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/sub"]
