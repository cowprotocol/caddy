# caddy

Upstream Caddy plus `request_decompress`, a handler that inflates gzip or deflate request bodies before they reach the upstream. The RPC proxies send OVH nodes deflate-compressed bodies to cut egress.

Every push to `main` builds the image and pushes `ghcr.io/cowprotocol/caddy:<caddy version>` (the version in the Dockerfile `FROM` line) plus a `sha-<commit>` tag. Bump Caddy by editing the `FROM` lines; the pin in the infrastructure repo (`ansible/roles/caddy`) follows via Renovate.

Caddyfile:

```
{
	order request_decompress after request_body
}
request_decompress 10MB
```

The argument caps the decoded body (default 10MiB); keep it next to `request_body max_size` so the two move together. Metric: `caddy_request_decompress_bytes_total{encoding, stage="wire"|"decoded"}`.
