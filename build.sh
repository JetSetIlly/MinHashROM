#!/bin/bash

goBinary="go"
gcflags="-c 3 -B -wb=false"
ldflags="-s -w"

GOARCH=amd64 GOOS=linux $goBinary build -gcflags "$gcflags" -ldflags "$ldflags" -o MinHashROM_linux_amd64
GOARCH=amd64 GOOS=windows $goBinary build -gcflags "$gcflags" -ldflags "$ldflags -H=windows" -o MinHashROM_windows_amd64

