FROM docker.io/library/golang:1.26-alpine AS controller
WORKDIR /src
COPY deploy/preview/go.mod deploy/preview/go.sum ./
RUN go mod download
COPY deploy/preview/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /preview-controller ./cmd/controller

FROM docker.io/library/golang:1.26-alpine AS bootstrap
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /preview-init ./cmd/preview-init

FROM docker.io/library/alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=controller /preview-controller /preview-controller
COPY --from=bootstrap /preview-init /preview-init
USER 1000:1000
ENTRYPOINT ["/preview-controller"]
