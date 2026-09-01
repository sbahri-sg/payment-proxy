# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .

FROM build-base AS build-payment-proxy
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/payment-proxy ./cmd/payment-proxy

FROM build-base AS build-connector-runner
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/connector-runner ./cmd/connector-runner

FROM build-base AS build-midtrans-provider-app
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/midtrans-provider-app ./cmd/midtrans-provider-app

FROM build-base AS build-duitku-provider-app
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/duitku-provider-app ./cmd/duitku-provider-app

FROM build-base AS build-doku-provider-app
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/doku-provider-app ./cmd/doku-provider-app

FROM build-base AS build-ipaymu-provider-app
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ipaymu-provider-app ./cmd/ipaymu-provider-app

FROM alpine:3.22 AS runtime-base
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
USER 10001:10001

FROM runtime-base AS connector-runner
COPY --from=build-connector-runner /out/connector-runner /usr/local/bin/connector-runner
EXPOSE 18081
ENTRYPOINT ["/usr/local/bin/connector-runner"]

FROM runtime-base AS midtrans-provider-app
COPY --from=build-midtrans-provider-app /out/midtrans-provider-app /usr/local/bin/midtrans-provider-app
EXPOSE 18083
ENTRYPOINT ["/usr/local/bin/midtrans-provider-app"]

FROM runtime-base AS duitku-provider-app
COPY --from=build-duitku-provider-app /out/duitku-provider-app /usr/local/bin/duitku-provider-app
EXPOSE 18084
ENTRYPOINT ["/usr/local/bin/duitku-provider-app"]

FROM runtime-base AS doku-provider-app
COPY --from=build-doku-provider-app /out/doku-provider-app /usr/local/bin/doku-provider-app
EXPOSE 18085
ENTRYPOINT ["/usr/local/bin/doku-provider-app"]

FROM runtime-base AS ipaymu-provider-app
COPY --from=build-ipaymu-provider-app /out/ipaymu-provider-app /usr/local/bin/ipaymu-provider-app
EXPOSE 18086
ENTRYPOINT ["/usr/local/bin/ipaymu-provider-app"]

FROM scratch AS midtrans-provider-app-artifact
COPY --from=build-midtrans-provider-app /out/midtrans-provider-app /connector

FROM scratch AS duitku-provider-app-artifact
COPY --from=build-duitku-provider-app /out/duitku-provider-app /connector

FROM scratch AS doku-provider-app-artifact
COPY --from=build-doku-provider-app /out/doku-provider-app /connector

FROM scratch AS ipaymu-provider-app-artifact
COPY --from=build-ipaymu-provider-app /out/ipaymu-provider-app /connector

FROM scratch AS xendit-provider-runtime-artifact
COPY --from=build-connector-runner /out/connector-runner /connector

# Backward-compatible build target. New Provider App ZIPs are source-only;
# this target exports the separately managed runtime executable.
FROM xendit-provider-runtime-artifact AS xendit-provider-app-artifact

FROM runtime-base AS payment-proxy
COPY --from=build-payment-proxy /out/payment-proxy /usr/local/bin/payment-proxy
EXPOSE 8080 19090
ENTRYPOINT ["/usr/local/bin/payment-proxy"]
CMD ["api"]
