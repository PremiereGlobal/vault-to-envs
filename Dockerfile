FROM golang:1.9 as builder
WORKDIR /go/src/github.com/readytalk/vault-to-envs/
RUN go get -d -v \
    github.com/hashicorp/vault/api \
    github.com/Sirupsen/logrus \
    github.com/kelseyhightower/envconfig
COPY src/ .
RUN CGO_ENABLED=0 GOOS=linux go build -v -a -o app .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /go/src/github.com/readytalk/vault-to-envs/ .
CMD ["./app"]
