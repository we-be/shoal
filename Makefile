.PHONY: build run run-cf stop clean

CONTROLLER_PORT := 8180
LP_BIN := $(shell which lightpanda)
TESTSITE_PORT := 9090
MINNOW_COUNT := 10

build:
	@mkdir -p bin
	go build -o bin/controller ./cmd/controller
	go build -o bin/agent ./cmd/agent
	go build -o bin/testsite ./examples/testsite
	@echo "built controller + agent + testsite"

# Lightpanda cluster (3 agents)
run: build stop
	@echo "launching shoal cluster (lightpanda)..."
	@bin/testsite -addr :$(TESTSITE_PORT) > /dev/null 2>&1 & echo "  testsite on :$(TESTSITE_PORT)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@for i in 0 1 2; do \
		agent_port=$$((8181 + $$i)); \
		lp_port=$$((9222 + $$i)); \
		bin/agent -addr :$$agent_port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend lightpanda \
			-lightpanda-bin $(LP_BIN) \
			-lightpanda-port $$lp_port \
			> /dev/null 2>&1 & \
		echo "  agent :$$agent_port -> lightpanda :$$lp_port"; \
	done
	@sleep 2
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool
	@echo "shoal is running. make stop to tear down."

# CF cluster: 1 Chrome grouper + N tls-client minnows
run-cf: build stop
	@rm -rf /tmp/shoal-chrome-*
	@echo "launching shoal CF cluster (1 grouper + $(MINNOW_COUNT) minnows)..."
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@bin/agent -addr :8181 \
		-controller http://localhost:$(CONTROLLER_PORT) \
		-backend chrome -lightpanda-port 9333 \
		> /dev/null 2>&1 & echo "  grouper on :8181 -> chrome :9333"
	@for i in $$(seq 0 $$(($(MINNOW_COUNT) - 1))); do \
		port=$$((8190 + $$i)); \
		bin/agent -addr :$$port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend tls-client \
			> /dev/null 2>&1 & \
		echo "  minnow on :$$port"; \
	done
	@sleep 8
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool
	@echo "shoal CF cluster is running. make stop to tear down."

stop:
	@fuser -k $(CONTROLLER_PORT)/tcp 8181/tcp 9333/tcp $(TESTSITE_PORT)/tcp \
		$$(seq -s '/tcp ' 8182 8200)/tcp \
		$$(seq -s '/tcp ' 9222 9225)/tcp \
		2>/dev/null || true
	@sleep 0.3
	@echo "stopped"

clean: stop
	rm -rf bin/ /tmp/shoal-chrome-*
