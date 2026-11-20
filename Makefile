.PHONY: help up recreate down install-cilium install-tetragon deploy test verify-mcp demo-gateway apply-cilium apply-tetragon hubble-flows events reset build-image load-image docker-build docker-push

KIND_CLUSTER ?= mcp-demo
KIND_CONFIG ?= kind-config.yaml
NAMESPACE ?= mcp-demo
GATEWAY_URL ?= http://localhost:30090/mcp
CONSOLE_URL ?= http://localhost:30090
HUBBLE_URL ?= http://localhost:31235
GATEWAY_IMAGE ?= ghcr.io/zeze1020/mcp-gateway:latest
USE_LOCAL_BUILD ?= 1
SCENARIO_DIR ?= mcp-enforcement
ASSETS_DIR ?= $(SCENARIO_DIR)/assets

help: ## Show available make targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

up: ## Provision 2-node Kind cluster + Cilium + Hubble UI + Tetragon + mcp-demo workloads
	@command -v kind >/dev/null 2>&1 || { echo "kind is required: https://kind.sigs.k8s.io/"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required"; exit 1; }
	@command -v helm >/dev/null 2>&1 || { echo "helm is required: https://helm.sh/"; exit 1; }
	@echo "==> [1/5] Ensuring 2-node Kind cluster ($(KIND_CLUSTER)) with $(KIND_CONFIG)..."
	@if kind get clusters | grep -qx $(KIND_CLUSTER); then \
		if kubectl -n kube-system get ds kindnet >/dev/null 2>&1; then \
			echo "⚠️  Existing '$(KIND_CLUSTER)' cluster was created with kindnet (without $(KIND_CONFIG))."; \
			echo "    Run 'make recreate' to replace it with a clean 2-node Cilium cluster + port mappings."; \
			exit 1; \
		fi; \
		echo "    Cluster '$(KIND_CLUSTER)' already exists (Cilium-ready)."; \
	else \
		kind create cluster --name $(KIND_CLUSTER) --config $(KIND_CONFIG); \
	fi
	@$(MAKE) --no-print-directory install-cilium
	@$(MAKE) --no-print-directory install-tetragon
	@if [ "$(USE_LOCAL_BUILD)" = "1" ]; then $(MAKE) --no-print-directory load-image; fi
	@$(MAKE) --no-print-directory deploy
	@echo ""
	@echo "✅ MCP Enforcement Kind cluster is ready!"
	@echo "   • MCP Console & Native MCP Apps UI : $(CONSOLE_URL)"
	@echo "   • MCP Streamable HTTP Endpoint     : $(GATEWAY_URL)"
	@echo "   • Cilium Hubble UI (Traffic/Drops) : $(HUBBLE_URL)"
	@echo ""

recreate: ## Delete existing Kind cluster and provision a fresh 2-node Cilium cluster
	-kind delete cluster --name $(KIND_CLUSTER)
	@$(MAKE) --no-print-directory up

install-cilium: ## Install Cilium CNI + Hubble Relay + Hubble UI (NodePort 31235)
	@echo "==> [2/5] Installing Cilium CNI + Hubble Relay + Hubble UI (NodePort 31235)..."
	@helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
	@helm repo update cilium
	helm upgrade --install cilium cilium/cilium \
		--namespace kube-system \
		--set image.pullPolicy=IfNotPresent \
		--set ipam.mode=kubernetes \
		--set operator.replicas=1 \
		--set envoy.enabled=false \
		--set hubble.enabled=true \
		--set hubble.relay.enabled=true \
		--set hubble.ui.enabled=true \
		--set hubble.ui.service.type=NodePort \
		--set hubble.ui.service.nodePort=31235
	@echo "    Waiting for Cilium agent, operator, Hubble Relay & Hubble UI rollouts..."
	kubectl -n kube-system rollout status ds/cilium --timeout=300s
	kubectl -n kube-system rollout status deploy/cilium-operator --timeout=180s
	kubectl -n kube-system rollout status deploy/hubble-relay --timeout=300s
	kubectl -n kube-system rollout status deploy/hubble-ui --timeout=300s
	kubectl wait --for=condition=Ready node --all --timeout=120s

install-tetragon: ## Install Tetragon eBPF runtime security via Helm
	@echo "==> [3/5] Installing Tetragon eBPF runtime security..."
	@helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
	helm upgrade --install tetragon cilium/tetragon \
		--namespace kube-system
	kubectl -n kube-system rollout status ds/tetragon --timeout=300s

test: ## Run Go unit tests for the MCP gateway, runner, backend & native MCP UIs
	go -C ./$(ASSETS_DIR)/gateway test -v ./...

docker-build: ## Build multi-mode MCP gateway/runner/backend container image locally
	docker build -t $(GATEWAY_IMAGE) ./$(ASSETS_DIR)/gateway

docker-push: ## Build and push multi-arch container image to GHCR
	./scripts/push-ghcr.sh

build-image: docker-build ## Alias for docker-build

load-image: build-image ## Build and load local image into Kind cluster
	@echo "==> [4/5] Loading $(GATEWAY_IMAGE) into Kind cluster ($(KIND_CLUSTER))..."
	kind load docker-image $(GATEWAY_IMAGE) --name $(KIND_CLUSTER)

deploy: ## Deploy mcp-demo workloads (approved-backend, mcp-tool-runner, mcp-gateway)
	@echo "==> [5/5] Deploying mcp-demo workloads..."
	kubectl apply -f $(ASSETS_DIR)/gateway/k8s.yaml
	kubectl -n $(NAMESPACE) rollout status deploy/approved-backend --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-tool-runner --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-gateway --timeout=180s

verify-mcp: ## Verify MCP initialize, tools/list, resources/list & native MCP App resource
	@echo "==> initialize"
	@curl -sS -X POST $(GATEWAY_URL) -H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' ; echo ""
	@echo "==> tools/list"
	@curl -sS -X POST $(GATEWAY_URL) -H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' ; echo ""
	@echo "==> resources/list (Native MCP Apps ui://)"
	@curl -sS -X POST $(GATEWAY_URL) -H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":3,"method":"resources/list"}' ; echo ""

demo-gateway: ## Run Layer 1 MCP gateway approval demo script
	./$(ASSETS_DIR)/scripts/trigger-high-risk.sh

apply-cilium: ## Apply Layer 2 CiliumNetworkPolicy and test egress block (view in Hubble UI)
	kubectl apply -f $(ASSETS_DIR)/policies/cnp-mcp-egress.yaml
	./$(ASSETS_DIR)/scripts/test-exfiltration.sh

apply-tetragon: ## Apply Layer 3 Tetragon runtime policy and test shell SIGKILL (exit 137)
	kubectl apply -f $(ASSETS_DIR)/policies/tp-block-shell.yaml
	./$(ASSETS_DIR)/scripts/test-shell-escape.sh

hubble-flows: ## Show recent Cilium Hubble flows (FORWARDED/DROPPED) for mcp-demo namespace
	@RUNNER_NODE=$$(kubectl -n $(NAMESPACE) get pod -l app=mcp-tool-runner -o jsonpath='{.items[0].spec.nodeName}'); \
	CILIUM_POD=$$(kubectl -n kube-system get pod -l k8s-app=cilium --field-selector "spec.nodeName=$${RUNNER_NODE}" -o jsonpath='{.items[0].metadata.name}'); \
	kubectl -n kube-system exec "$${CILIUM_POD}" -c cilium-agent -- hubble observe --namespace $(NAMESPACE) --last 25 || true

events: ## Show recent Tetragon enforcement events from mcp-tool-runner's node
	@RUNNER_NODE=$$(kubectl -n $(NAMESPACE) get pod -l app=mcp-tool-runner -o jsonpath='{.items[0].spec.nodeName}'); \
	TETRA_POD=$$(kubectl -n kube-system get pod -l app.kubernetes.io/name=tetragon --field-selector "spec.nodeName=$${RUNNER_NODE}" -o jsonpath='{.items[0].metadata.name}'); \
	kubectl -n kube-system exec "$${TETRA_POD}" -c tetragon -- tetra getevents -o compact --namespace $(NAMESPACE) --since 10m || true

reset: ## Remove demo workloads and Layer 2/3 policies (keeps Kind cluster & Cilium/Tetragon)
	-kubectl delete -f $(ASSETS_DIR)/policies/tp-block-shell.yaml --ignore-not-found
	-kubectl delete -f $(ASSETS_DIR)/policies/cnp-mcp-egress.yaml --ignore-not-found
	-kubectl delete -f $(ASSETS_DIR)/gateway/k8s.yaml --ignore-not-found
	@echo "Reset complete. Run 'make deploy' to re-deploy clean workloads, or 'make down' to delete Kind cluster."

down: ## Delete the local Kind cluster completely
	kind delete cluster --name $(KIND_CLUSTER)
