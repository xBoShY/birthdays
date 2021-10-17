#!/bin/bash

service=$1
username=$2

curl -v \
"$service/$username"

echo
