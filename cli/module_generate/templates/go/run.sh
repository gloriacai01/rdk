#!/bin/sh
cd `dirname $0`

MODULE=$(basename "$PWD")

rm -rf go.mod go.sum
go mod init $MODULE
go get go.viam.com/rdk@latest
go mod tidy

go build -o bin/$MODULE main.go

# tar czf module.tar.gz bin/$MODULE
rm bin/$MODULE
echo "Starting module..."
exec go run main.go $@