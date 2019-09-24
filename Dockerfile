FROM golang:1.12-alpine AS build-stage

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

RUN CGO_ENABLED=0 go build -o ./tls-host-controller --ldflags "-w -extldflags '-static'" mutator.go

COPY mutator.go ./

# Final image.
FROM alpine:latest
RUN apk --no-cache add \
  ca-certificates
COPY --from=build-stage /app/main /usr/local/bin/tls-host-controller
ENTRYPOINT ["/usr/local/bin/tls-host-controller"]
