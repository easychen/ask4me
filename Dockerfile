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
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /data
COPY --from=go-builder /app/ask4me /usr/local/bin/ask4me
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
