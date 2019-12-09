FROM golang:1.13-alpine as builder

# Set Environment Variables
ENV HOME /app
ENV CGO_ENABLED 0
ENV GOOS linux

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go cn.go ./

# Build app
RUN go build -a -installsuffix cgo -o tls-host-controller .

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/tls-host-controller .

EXPOSE 4443

ENTRYPOINT [ "./tls-host-controller" ]
