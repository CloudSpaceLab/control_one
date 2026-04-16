FROM golang:1.24-bullseye AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/nodeagent ./cmd/nodeagent

FROM gcr.io/distroless/base-debian12

COPY --from=builder /out/nodeagent /usr/local/bin/nodeagent
COPY configs/example-config.yaml /etc/control-one/nodeagent.yaml

ENTRYPOINT ["/usr/local/bin/nodeagent", "-config", "/etc/control-one/nodeagent.yaml"]
