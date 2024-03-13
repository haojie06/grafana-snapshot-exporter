FROM golang:latest AS builder
WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -o grafana-snapshot-exporter \
    main.go

FROM chromedp/headless-shell:latest
WORKDIR /app
COPY --from=builder /app/grafana-snapshot-exporter /app/grafana-snapshot-exporter

ENTRYPOINT [ "/app/grafana-snapshot-exporter" ]