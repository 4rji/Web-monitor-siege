# Docker only packages the Go server together with the k6 binary.
# At runtime the server runs `k6 run` directly inside this same container.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/k6-web ./cmd/server

# Official k6 image, used only as a source for the k6 binary.
FROM grafana/k6:2.3.0 AS k6

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 app
COPY --from=k6 /usr/bin/k6 /usr/bin/k6
COPY --from=build /out/k6-web /usr/local/bin/k6-web
USER app
ENV ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/k6-web"]
