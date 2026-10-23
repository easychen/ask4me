FROM node:20-alpine AS ui-builder
WORKDIR /app/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.25-alpine AS go-builder
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

FROM alpine:3.21
# 2026-10 eMudhra 事件：ca-certificates 若早于 2026-10，仍含被攻破的 emSign ECC Root CA - G3。
# 直接删根 + 重建 bundle，并把「已删除」写成构建期断言。
# 注意：Alpine 的 update-ca-certificates --fresh 不会清理指向已删证书的悬空软链（ca-cert-*.pem），
# 必须显式删除，否则 `ls /etc/ssl/certs` 仍能看到它。
RUN apk add --no-cache ca-certificates tzdata \
 && rm -f /usr/share/ca-certificates/mozilla/emSign_ECC_Root_CA_-_G3.crt \
 && update-ca-certificates --fresh \
 && rm -f /etc/ssl/certs/*emSign_ECC_Root_CA_-_G3* \
 && ! ls /etc/ssl/certs | grep -q emSign_ECC_Root_CA_-_G3
WORKDIR /data
COPY --from=go-builder /app/ask4me /usr/local/bin/ask4me
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
