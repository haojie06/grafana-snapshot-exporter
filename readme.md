Create Grafana snapshot via headless chrome

```bash
docker run -it -p 8080:8080 \
    -e GRAFANA_URL=http://grafana:3000 \
    -e GRAFANA_USER=admin \
    -e GRAFANA_PASSWORD=admin \
    -e HEADLESS=true \
    -e CHROME_LOG=false \
    -e API_KEY=hello \
    -e ADDR=127.0.0.1:8080
```
