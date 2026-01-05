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
git clone https://github.com/peakyragnar/subluminal.git
cd subluminal
brew install --HEAD --formula ./Formula/sub.rb
```

### Docker

```bash
docker run --rm subluminal/sub:latest version
```
