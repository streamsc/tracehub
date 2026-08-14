FROM mtw6hbbpueh4iz.xuanyuan.run/library/golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/tracehub ./cmd/tracehub

FROM mtw6hbbpueh4iz.xuanyuan.run/library/alpine:3.23
RUN addgroup -g 10001 -S tracehub && adduser -u 10001 -S -G tracehub tracehub
RUN mkdir -p /var/lib/tracehub && chown tracehub:tracehub /var/lib/tracehub
COPY --from=build /out/tracehub /usr/local/bin/tracehub
USER tracehub
VOLUME ["/var/lib/tracehub"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tracehub"]
CMD ["serve", "--config", "/etc/tracehub/server.json"]
