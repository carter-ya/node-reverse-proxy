FROM golang:1.21.4-alpine3.18 AS builder

WORKDIR /go/src
COPY . /go/src

RUN ["go", "build", "-trimpath", "-o", "/go/build/node-reverse-proxy"]


FROM alpine:3.18

COPY --from=builder /go/build/node-reverse-proxy /usr/bin/node-reverse-proxy

WORKDIR /root

EXPOSE 8080

CMD ["node-reverse-proxy"]
