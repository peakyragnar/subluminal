# Subluminal

Subluminal is a local data-plane that intercepts agent tool execution, enforces
policy, and records an auditable ledger.

## Install

### From Source

```bash
go install github.com/peakyragnar/subluminal/cmd/sub@latest
```

### Homebrew (from repo)

```bash
# Clone and install from local formula
git clone https://github.com/peakyragnar/subluminal.git
brew install --HEAD --formula subluminal/Formula/sub.rb
```

### Docker

```bash
docker run --rm subluminal/sub:latest version
```
