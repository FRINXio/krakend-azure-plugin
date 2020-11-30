#!/bin/sh
go build -trimpath -buildmode=plugin -o azure_plugin.so
# mv azure_plugin.so ~/deleteme/testconfig
