#!/bin/bash

# Copyright 2024 Canonical, Ltd.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

DNS_NAME="${1:-mutating-pebble-webhook-svc.pebble-webhook.svc}"
COMMON_NAME="$(echo $DNS_NAME | cut -d. -f1)"

mkdir -p tls

openssl genrsa -out tls/ca.key 2048

openssl req -new -x509 \
  -subj "/C=AU/CN=${COMMON_NAME}" \
  -days 365 \
  -key tls/ca.key \
  -out tls/ca.crt

openssl req -newkey rsa:2048 -nodes \
  -subj "/C=AU/CN=${COMMON_NAME}" \
  -keyout tls/server.key \
  -out tls/server.csr

openssl x509 -req \
  -extfile <(printf "subjectAltName=DNS:${DNS_NAME}") \
  -days 365 \
  -in tls/server.csr \
  -CA tls/ca.crt -CAkey tls/ca.key -CAcreateserial \
  -out tls/server.crt
