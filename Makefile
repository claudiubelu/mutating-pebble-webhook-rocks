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

KUBECTL ?= kubectl

all: generate-selfsigned-cert generate-webhook-secret set-webhook-cabundle deploy-webhook

generate-selfsigned-cert:
	bash -x ./scripts/generate-cert.sh

generate-webhook-secret:
	$(KUBECTL) create secret tls -n pebble-webhook mutating-pebble-webhook-tls \
	  --cert=tls/server.crt \
	  --key=tls/server.key \
	  --dry-run=client \
	  -o yaml > ./manifests/webhook-secret.yaml

set-webhook-cabundle:
	sed -i -e 's/caBundle: .*/caBundle: $(shell base64 -w 0 tls/ca.crt)/' manifests/webhook.yaml

deploy-webhook:
	# Create the namespace first.
	$(KUBECTL) apply -f ./manifests/webhook-ns.yaml
	for file in $(shell ls ./manifests); do \
	  $(KUBECTL) apply -f "./manifests/$${file}"; \
	done

clean:
	rm -rf tls
	sed -i -e 's/tls.crt: .*/tls.crt: LS0t.../' manifests/webhook-secret.yaml
	sed -i -e 's/tls.key: .*/tls.key: LS0t.../' manifests/webhook-secret.yaml
	sed -i -e 's/caBundle: .*/caBundle: LS0t.../' manifests/webhook.yaml

remove-webhook:
	# Removing the namespace will remove everything from it.
	$(KUBECTL) delete -f ./manifests/webhook-ns.yaml
	$(KUBECTL) delete -f ./manifests/webhook.yaml

.PHONY: all generate-selfsigned-cert generate-webhook-secret set-webhook-cabundle deploy-webhook clean remove-webhook
