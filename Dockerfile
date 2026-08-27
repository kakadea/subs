FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETARCH
RUN target_arch="${TARGETARCH:-$(go env GOARCH)}" \
    && CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" \
    go build -trimpath -ldflags="-s -w" -o /out/subs ./cmd/server

FROM alpine:3.20

RUN addgroup -S subs && adduser -S -G subs -u 10001 subs \
    && apk add --no-cache ca-certificates tzdata su-exec \
    && mkdir -p /app /data/subtitles /data/temp /data/quarantine \
    && chown -R subs:subs /app /data

WORKDIR /app
COPY --from=build /out/subs /app/subs
COPY deploy/entrypoint.sh /usr/local/bin/subs-entrypoint
RUN chmod 0755 /app/subs /usr/local/bin/subs-entrypoint

ENV ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/subs-entrypoint"]
