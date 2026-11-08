FROM node:20-alpine AS ui-builder
WORKDIR /app/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.27.3-alpine AS go-builder
RUN apk add --no-cache ca-certificates tzdata
# GitHub Actions uses proxy.golang.org (default). Override locally in China:
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct ...
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /app/ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ask4me .
RUN mkdir -p /out/data/tmp && chown -R 65532:65532 /out/data

# Minimal runtime: static binary + CA roots + zoneinfo, no shell/package manager.
# Build with: docker build --target slim -t ask4me:slim .
FROM scratch AS slim
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder --chown=65532:65532 /out/data /data
COPY --from=go-builder /app/ask4me /ask4me
ENV TMPDIR=/data/tmp
WORKDIR /data
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/ask4me"]

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /data
COPY --from=go-builder /app/ask4me /usr/local/bin/ask4me
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
