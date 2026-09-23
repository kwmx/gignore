#!/bin/sh
# Generates gzipped man pages into ./manpages for release packaging.
set -e
rm -rf manpages
go run ./cmd/gignore gen-man manpages
gzip -9 -n manpages/*.1
