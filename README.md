# certpilot-agent

The [CertPilot](https://github.com/certpilot/certpilot) host agent. One binary
that runs on the machines where certificates are actually served: it inventories
what the host holds, requests certificates for it, and installs them where the
service reads them.

Its reason for existing is one sentence:

> **The agent generates its own private keys and never sends them anywhere.**
> CertPilot cannot produce them and does not claim to.

Everything else follows from that. A CSR leaves the host; the private half of
the key does not, and there is no code path that would let it.

```
go install github.com/certpilot/certpilot-agent/cmd@latest
```

```
docker run --rm -v certpilot-agent:/var/lib/certpilot-agent \
    ghcr.io/certpilot/agent:latest --help
```

## Commands

```
certpilot-agent enrol  --server=URL --token=TOKEN [--name=NAME] [--interval=5m]
certpilot-agent run    [--state-dir=DIR] [--installs=FILE]
certpilot-agent scan   [--path=DIR ...]      what this host would report
certpilot-agent request --name=HOST [--name=...] [--key-type=ECDSA]
certpilot-agent install [--installs=FILE] [--force] [--offline]
certpilot-agent profiles [NAME] [--detect]   which platforms are covered
certpilot-agent status [--state-dir=DIR]
certpilot-agent version
```

Enrolment generates the host's identity key locally and registers the public
half. Every later request is signed with it — HTTP and JSON, signed with
ed25519, using the scheme published in
[`certpilot-agent-sdk`](https://github.com/certpilot/certpilot-agent-sdk).

## Where it runs

| Target | State |
|:---|:---|
| Linux | Supported. Every deployment profile is tested against a real service |
| Windows | Supported for file destinations and for the certificate store, which is what IIS reads |
| macOS, FreeBSD | Compiles, and is usable for development. Not tested, and the deployment profiles assume systemd |

On Windows the private key guarantee is an **ACL**, not a file mode — Go accepts
a `0600` there and ignores it. The agent sets an explicit one on every key it
writes.

## Deployment profiles

A profile says where a service reads its certificate, what reloads it, and what
checks the configuration first, so a destination can name a platform instead of
four paths.

| | Verified against |
|:---|:---|
| `nginx` | nginx 1.27.5 |
| `apache` | Apache 2.4.68 |
| `haproxy` | HAProxy 2.6.12 |
| `caddy` | Caddy 2.11.4 |
| `postgresql` | PostgreSQL 16.15 |
| `postfix` | Postfix 3.7.11 |
| `dovecot` | Dovecot 2.3.19.1 |
| `mariadb` | MariaDB 10.11.18 |
| `tomcat` | Apache Tomcat 10.1.59 on JDK 21 |
| `iis` | IIS 10.0 on Windows Server 2025 |

**Those versions are measured, not remembered.** `make verify-profiles` starts
each service in a container, installs a certificate through this agent's own
installer, and completes a TLS handshake to confirm the service is serving it
after its own reload. IIS cannot run in a container, so CI runs it on a Windows
runner and checks the version in the catalogue against the web server that
actually ran the test.

## Building and checking

```
make build              the binary
make test               the tests
make lint               go vet, gofmt, staticcheck
make verify-profiles    install to every platform, in containers
```

`make verify-profiles` needs a container runtime. Nothing else here does.

### What CI asks that a unit test cannot

- **`agent on Windows`** — the ACL that replaces the file mode, on a real
  Windows kernel. Cross-compiling proves it builds, which is the half that would
  pass while every key on the host was readable by everyone.
- **`IIS end to end`** — install, renew and roll back against a running IIS,
  proving the certificate it serves changed.
- **`this agent against the core`** — the contract, run rather than assumed. The
  core's repository holds `scripts/agent-lifecycle.sh`, which enrols, requests,
  installs and reports against a real core and a real gateway; this job points
  it at the agent built from the commit under review. Either side can break a
  contract between two repositories, so both sides ask.

## Documentation

The guide lives at
**https://certpilot.github.io/certpilot-docs/agent** — enrolment, grants,
installation specs, the inventory, and a page per platform.

## Releases

Images publish on a tag, never on a merge, so `latest` means the most recent
release rather than the most recent commit. Pin anyway for anything you depend
on.

The Go module is tagged in step with the image, so
`go install github.com/certpilot/certpilot-agent/cmd@v0.2.0` installs the same
code the image contains.

## Licence

Apache 2.0.
