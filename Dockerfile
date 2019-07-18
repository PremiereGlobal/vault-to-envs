FROM golang:1.12 as builder

ARG GOOS=linux

WORKDIR /src

COPY . .

RUN CGO_ENABLED=0 GOOS=${GOOS} go build -v -a -mod vendor -o v2e .

# Stage 2

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /usr/local/bin

COPY --from=builder /src/v2e .

CMD ["./v2e"]
