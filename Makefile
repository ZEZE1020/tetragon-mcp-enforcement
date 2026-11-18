.PHONY: up demo-gateway apply-cilium apply-tetragon events reset build-image load-image deploy docker-build docker-push

KIND_CLUSTER ?= mcp-demo
NAMESPACE ?= mcp-demo
GATEWAY_URL ?= http://localhost:30090/mcp
GATEWAY_IMAGE ?= ghcr.io/zeze1020/mcp-gateway:latest
SCENARIO_DIR ?= mcp-enforcement
ASSETS_DIR ?= $(SCENARIO_DIR)/assets

up: ## Create local Kind cluster and deploy demo workloads (pulls published images)
	@command -v kind >/dev/null 2>&1 || { echo "kind is required: https://kind.sigs.k8s.io/"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required"; exit 1; }
	@kind get clusters | grep -qx $(KIND_CLUSTER) || kind create cluster --name $(KIND_CLUSTER)
	@$(MAKE) deploy
	@echo "Cluster ready. Gateway expected at $(GATEWAY_URL) once port-mapping/NodePort is reachable."

docker-build: ## Build MCP gateway container image locally
	docker build -t $(GATEWAY_IMAGE) ./$(ASSETS_DIR)/gateway

docker-push: ## Build and push container image(s) to GHCR
	./scripts/push-ghcr.sh

build-image: docker-build ## Alias for docker-build

load-image: build-image ## Load locally built image into Kind (for local dev)
	kind load docker-image $(GATEWAY_IMAGE) --name $(KIND_CLUSTER)

deploy: ## Deploy lab workloads to Kubernetes
	kubectl apply -f $(ASSETS_DIR)/gateway/k8s.yaml
	kubectl -n $(NAMESPACE) rollout status deploy/approved-backend --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-gateway --timeout=180s
	kubectl -n $(NAMESPACE) rollout status deploy/mcp-tool-runner --timeout=180s

demo-gateway: ## Run the gateway approval demo script
	./$(ASSETS_DIR)/scripts/trigger-high-risk.sh

apply-cilium: ## Apply CiliumNetworkPolicy for tool-runner egress restriction
	kubectl apply -f $(ASSETS_DIR)/policies/cnp-mcp-egress.yaml
	./$(ASSETS_DIR)/scripts/test-exfiltration.sh

apply-tetragon: ## Apply Tetragon runtime policy to block shells in tool-runner
	kubectl apply -f $(ASSETS_DIR)/policies/tp-block-shell.yaml
	./$(ASSETS_DIR)/scripts/test-shell-escape.sh

events: ## Show recent Tetragon events for mcp-demo namespace (requires tetra in tetragon pod)
	kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --namespace $(NAMESPACE) --since 10m || true

reset: ## Remove demo resources and policies
	-kubectl delete -f $(ASSETS_DIR)/policies/tp-block-shell.yaml --ignore-not-found
	-kubectl delete -f $(ASSETS_DIR)/policies/cnp-mcp-egress.yaml --ignore-not-found
	-kubectl delete -f $(ASSETS_DIR)/gateway/k8s.yaml --ignore-not-found
	@echo "Reset complete. To delete the Kind cluster: kind delete cluster --name $(KIND_CLUSTER)"
