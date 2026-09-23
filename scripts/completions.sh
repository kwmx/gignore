#!/bin/sh
# Generates shell completion scripts into ./completions for release packaging.
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish powershell; do
	go run ./cmd/gignore completion "$sh" >"completions/gignore.$sh"
done
mv completions/gignore.powershell completions/gignore.ps1
