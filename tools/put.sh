#!/bin/bash

service=$1
username=$2
dateOfBirth=$3

curl -v --header "Content-Type: application/json" \
--request PUT \
--data "{\"dateOfBirth\": \"$dateOfBirth\"}" \
"$service/$username"
