FROM golang:1.22-alpine3.20 as builder

COPY ./op-signer-mon /app

WORKDIR /app
RUN apk add --no-cache make jq bash git alpine-sdk
RUN make build

FROM alpine:3.20
RUN apk --no-cache add make jq bash git alpine-sdk redis

RUN addgroup -S app && adduser -S app -G app
USER app
WORKDIR /app

COPY --from=builder /app/bin/op-signer-mon /app

ENTRYPOINT ["/app/op-signer-mon"]
