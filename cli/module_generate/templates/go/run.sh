#!/bin/sh
cd `dirname $0`

MODULE=$(basename "$PWD")
export PATH=$PATH:$(go env GOPATH)/bin

rm -rf go.mod go.sum
go mod init $MODULE  > /dev/null 2>&1
echo "Downloading necessary go packages..."
if ! ( 
    go get go.viam.com/rdk@latest  > /dev/null 2>&1
    go get golang.org/x/tools/cmd/goimports@latest  > /dev/null 2>&1
    gofmt -w -s .
    go mod tidy  > /dev/null 2>&1
    goimports -w models/module.go 
); then
    exit 1
fi
go build -o bin/$MODULE main.go

# tar czf module.tar.gz bin/$MODULE
rm bin/$MODULE
echo "Starting module..."
exec go run main.go $@

