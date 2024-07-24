FROM golang:alpine as builder

WORKDIR  /workspace

COPY ./ ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -mod=vendor -o bin/raccoon cmd/raccoon/raccoon.go && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -mod=vendor -o bin/raccoond cmd/raccoond/raccoond.go

FROM alpine
RUN apk update && apk add --no-cache iptables
WORKDIR /
COPY --from=builder /workspace/bin/* /