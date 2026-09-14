# ExternalDNS BlueCat webhook

Go webhook provider for [ExternalDNS](https://github.com/kubernetes-sigs/external-dns) that manages records in **BlueCat Address Manager** using **REST API v2**.

ExternalDNS no longer accepts new in-tree providers. This sidecar implements the [webhook provider API](https://kubernetes-sigs.github.io/external-dns/latest/docs/tutorials/webhook-provider/) so ExternalDNS can sync Kubernetes `Service` / `Ingress` hostnames into BAM without BlueCat Gateway.

The implementation is informed by the in-tree Gateway-based BlueCat provider and by [m1schka-bdr/external-dns](https://github.com/kubernetes-sigs/external-dns/compare/master...m1schka-bdr:external-dns:master) (an in-tree v2 attempt). This repo keeps the provider out-of-tree and talks to `/api/v2` directly.

## Record types

| ExternalDNS | BlueCat v2 |
| --- | --- |
| A / AAAA | `HostRecord` (IPv4 vs IPv6 split on read) |
| CNAME | `AliasRecord` |
| TXT | `TXTRecord` |

Unsupported types are ignored.

## Build

```bash
go build -o bin/external-dns-bluecat-webhook ./cmd/webhook
go test ./...
```

## Configuration

Flags and environment variables are equivalent. Credentials from the environment always win over a JSON file.

| Flag | Env | Description |
| --- | --- | --- |
| `--bluecat-host` | `BLUECAT_HOST` | BAM base URL, e.g. `https://bam.example.com` |
| `--bluecat-username` | `BLUECAT_USERNAME` | API user |
| `--bluecat-password` | `BLUECAT_PASSWORD` | API password |
| `--bluecat-root-zone` | `BLUECAT_ROOT_ZONE` | Zone discovery filter (`absoluteName:contains(...)`) |
| `--bluecat-dns-view` | `BLUECAT_DNS_VIEW` | Optional view name filter |
| `--bluecat-dns-deploy-type` | `BLUECAT_DNS_DEPLOY_TYPE` | `no-deploy` (default), `quick-deploy`, or `dynamic` |
| `--bluecat-dns-server-name` | `BLUECAT_DNS_SERVER_NAME` | When set with `quick-deploy`, POST a zone deployment after changes |
| `--bluecat-skip-tls-verify` | `BLUECAT_SKIP_TLS_VERIFY` | Skip TLS verify (labs only; incompatible with `--bluecat-ca-file`) |
| `--bluecat-ca-file` | `BLUECAT_CA_FILE` | PEM file of extra CA certificates to trust for BAM TLS |
| `--bluecat-config-file` | `BLUECAT_CONFIG_FILE` | JSON file using the same keys as the old in-tree provider |
| `--domain-filter` | | Limit managed domains |
| `--listen-address` | | Webhook API, default `127.0.0.1:8888` |
| `--health-address` | | `/healthz` and `/readyz`, default `:8080` |
| `--dry-run` | | Log changes only |

JSON file example:

```json
{
  "bluecatHost": "https://bam.example.com",
  "bluecatUsername": "api",
  "bluecatPassword": "secret",
  "dnsView": "Internal",
  "rootZone": "example.com",
  "dnsDeployType": "no-deploy",
  "caFile": "/etc/bluecat/ca.crt",
  "skipTLSVerify": false
}
```

Auth uses `POST /api/v2/sessions`, then `Authorization: Basic base64(username:apiToken)` and `Accept: application/hal+json`.

## Run with ExternalDNS

Recommended layout: this process as a **localhost sidecar** next to ExternalDNS.

Webhook:

```bash
external-dns-bluecat-webhook \
  --bluecat-host=https://bam.example.com \
  --bluecat-username="$BLUECAT_USERNAME" \
  --bluecat-password="$BLUECAT_PASSWORD" \
  --bluecat-root-zone=example.com \
  --bluecat-ca-file=/etc/bluecat/ca.crt \
  --domain-filter=example.com \
  --listen-address=127.0.0.1:8888
```

ExternalDNS:

```bash
external-dns \
  --source=ingress \
  --source=service \
  --provider=webhook \
  --webhook-provider-url=http://127.0.0.1:8888 \
  --txt-owner-id=cluster-1 \
  --policy=upsert-only
```

See `deploy/external-dns-bluecat.yaml` for a Kubernetes example.

## Status

This is a starting point for developing against a real BAM. Next useful work:

- Pagination on zone and record list calls
- Filtering zones by configuration / view via the v2 filter language
- Treating A and AAAA as one HostRecord on update so dual-stack hosts are not deleted twice
- OpenAPI-generated client once a BAM v2 spec is checked in
