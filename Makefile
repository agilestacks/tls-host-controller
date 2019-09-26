.DEFAULT_GOAL := deploy

export COMPONENT_NAME ?= tls-host-controller
export NAMESPACE      ?= $(error NAMESPACE must be set)
export DOMAIN_NAME    ?= $(error DOMAIN_NAME must be set)
REGISTRY              ?= agilestacks
IMAGE                 ?= $(REGISTRY)/$(COMPONENT_NAME)
IMAGE_VERSION         ?= $(shell git rev-parse HEAD | colrm 7)

docker       := docker
kubectl      := kubectl --context="$(DOMAIN_NAME)" --namespace="$(NAMESPACE)"

deploy: purge create

undeploy: purge

create:
	deploy/create_cm_issuer_and_cert.sh
	$(kubectl) apply -f deploy/manifests.yaml

purge:
	-$(kubectl) delete certificate tls-host-controller
	-$(kubectl) delete secret tls-host-controller-certs
	-$(kubectl) delete secret cm-util-ca
	-$(kubectl) delete mutatingwebhookconfiguration tls-host-controller
	-$(kubectl) delete issuer util-ca
	-$(kubectl) delete -f deploy/manifests.yaml

build:
	$(docker) build -f Dockerfile -t $(IMAGE):$(IMAGE_VERSION) .
	$(docker) tag  $(IMAGE):$(IMAGE_VERSION) $(IMAGE):latest
.PHONY: build

push:
	$(docker) push $(IMAGE):$(IMAGE_VERSION)
	$(docker) push $(IMAGE):latest
.PHONY: push

