---
title: "Installation"
description: "Install dailymed from a release, with go install, or from source."
weight: 20
---

## Prebuilt binaries

Every [release](https://github.com/tamnd/dailymed-cli/releases) carries archives for Linux, macOS,
and Windows on amd64 and arm64, plus deb, rpm, and apk packages for Linux.
Download, unpack, put `dailymed` on your `PATH`, done. The `checksums.txt`
on each release is signed with keyless [cosign](https://docs.sigstore.dev/) if
you want to verify before running.

## With Go

```bash
go install github.com/tamnd/dailymed-cli/cmd/dailymed@latest
```

That puts `dailymed` in `$(go env GOPATH)/bin`, which is `~/go/bin` unless
you moved it. Make sure that directory is on your `PATH`.

## From source

```bash
git clone https://github.com/tamnd/dailymed-cli
cd dailymed-cli
make build        # produces ./bin/dailymed
./bin/dailymed version
```

## Container image

```bash
docker run --rm ghcr.io/tamnd/dailymed:latest --help
```

## Checking the install

```bash
dailymed version
```

prints the version and exits.
