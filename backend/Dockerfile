# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/payment-proxy ./cmd/payment-proxy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/connector-runner ./cmd/connector-runner
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/midtrans-provider-app ./cmd/midtrans-provider-app

FROM scratch AS midtrans-provider-app-artifact
COPY --from=build /out/midtrans-provider-app /connector

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
COPY --from=build /out/payment-proxy /usr/local/bin/payment-proxy
COPY --from=build /out/connector-runner /usr/local/bin/connector-runner
COPY --from=build /out/midtrans-provider-app /usr/local/bin/midtrans-provider-app
USER 10001:10001
EXPOSE 8080 18081 18083
ENTRYPOINT ["/usr/local/bin/payment-proxy"]
CMD ["api"]
