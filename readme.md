# Create Grafana snapshot via headless chrome

## HowTo

```bash
docker run -it -p 8080:8080 \
    -e GRAFANA_URL=http://grafana:3000 \
    -e GRAFANA_USERNAME=admin \
    -e GRAFANA_PASSWORD=admin \
    -e HEADLESS=true \
    -e CHROME_LOG=false \
    -e API_KEY=hello \
    -e ADDR=127.0.0.1:8080 \
    --name grafana-snapshot-exporter \
    --rm \
    ghcr.io/haojie06/grafana-snapshot-exporter:latest
```

You can use the demo exporter endpoint `https://grafana-snapshot-exporter.sailing.im` or deploy your own exporter.

```bash
# Create snapshot (using your default grafana url and username/password)
curl --location 'https://grafana-snapshot-exporter.sailing.im/snapshot' \
--header 'Content-Type: application/json' \
--header 'X-API-Key: hello' \
--data '{
    "dashboard_id": "b05cf7ef-3094-4192-9471-80e6b403b2d7",
    "query": "orgId=1&var-group=public",
    "from": 1710172800000,
    "to": 1710259199000
}'

# Login and create snapshot in an isolated browser (using your custom grafana url and username/password)
curl --location 'https://grafana-snapshot-exporter.sailing.im/login_and_snapshot' \
--header 'Content-Type: application/json' \
--header 'X-API-Key: hello' \
--data '{
    "grafana_url": "https://grafana.example.com",
    "username": "admin",
    "password": "password",
    "dashboard_id": "b05cf7ef-xxxx-4192-9471-80e6b403b2d7",
    "query": "orgId=1&var-name=test_name",
    "from": 1710172800000,
    "to": 1710259199000
}'
```
