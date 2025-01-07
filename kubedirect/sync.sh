#! /usr/bin/env bash

mv pkg pkg.sym
cp -Lr pkg.sym pkg
go mod tidy
rm -r pkg
mv pkg.sym pkg