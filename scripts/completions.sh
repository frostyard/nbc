#!/bin/sh
set -e
rm -rf completions
mkdir completions
go build -o nbc .
for sh in bash zsh fish; do
  ./nbc completion "$sh" >"completions/nbc.$sh"
done