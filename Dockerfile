FROM golang:1.16-alpine as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN go build github.com/agilestacks/tls-host-controller/cmd/tls-host-controller

FROM alpine:3.13
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/tls-host-controller /bin/
EXPOSE 4443
ENTRYPOINT ["/bin/tls-host-controller"]
