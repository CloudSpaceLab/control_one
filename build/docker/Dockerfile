FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/controlone-agent ./cmd/nodeagent

FROM gcr.io/distroless/base-debian12

COPY --from=builder /out/controlone-agent /usr/local/bin/controlone-agent
COPY configs/example-config.yaml /etc/control-one/nodeagent.yaml

ENTRYPOINT ["/usr/local/bin/controlone-agent", "--config", "/etc/control-one/nodeagent.yaml"]
