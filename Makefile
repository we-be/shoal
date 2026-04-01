.PHONY: build stop clean school school-lp school-sp school-cf school-minnow

CONTROLLER_PORT := 8180
LP_BIN := $(shell which lightpanda)
SP_BIN := $(shell which stealthpanda 2>/dev/null || echo stealthpanda)
TESTSITE_PORT := 9090
COUNT := 5
VERSION := $(shell head -1 CHANGELOG.md | grep -oP 'v\d+\.\d+\.\d+' || echo "dev")
LDFLAGS := -X github.com/we-be/shoal/internal/api.Version=$(VERSION)

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/controller ./cmd/controller
	go build -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent
	go build -o bin/testsite ./examples/testsite
	@echo "built controller (xiphosura) + agent (mullet) $(VERSION)"

# === Formations ===
# Each formation is a different mix of agents for different workloads.
# Use COUNT=N to set the number of agents (default 5).

# School of minnows — pure HTTP, no browser. For APIs, RSS, JSON endpoints.
school-minnow: build stop
	@rm -f shoal-pool.json
	@echo "formation: school of minnows ($(COUNT) tls-client)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@for i in $$(seq 0 $$(($(COUNT) - 1))); do \
		port=$$((8190 + $$i)); \
		bin/agent -addr :$$port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend tls-client \
			> /dev/null 2>&1 & \
		echo "  minnow on :$$port"; \
	done
	@sleep 2
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool

# School of redfish — Lightpanda browsers. For JS-rendered pages without CF.
school-lp: build stop
	@rm -f shoal-pool.json
	@echo "formation: school of redfish ($(COUNT) lightpanda)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@for i in $$(seq 0 $$(($(COUNT) - 1))); do \
		agent_port=$$((8181 + $$i)); \
		lp_port=$$((9222 + $$i)); \
		bin/agent -addr :$$agent_port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend lightpanda \
			-lightpanda-bin $(LP_BIN) \
			-lightpanda-port $$lp_port \
			> /dev/null 2>&1 & \
		echo "  redfish :$$agent_port -> lightpanda :$$lp_port"; \
	done
	@sleep 2
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool

# School of stealthpandas — stealth headless browser. For JS + anti-bot sites.
school-sp: build stop
	@rm -f shoal-pool.json
	@echo "formation: school of stealthpandas ($(COUNT) stealthpanda)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@for i in $$(seq 0 $$(($(COUNT) - 1))); do \
		agent_port=$$((8181 + $$i)); \
		sp_port=$$((9222 + $$i)); \
		bin/agent -addr :$$agent_port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend stealthpanda \
			-stealthpanda-bin $(SP_BIN) \
			-lightpanda-port $$sp_port \
			> /dev/null 2>&1 & \
		echo "  stealthpanda :$$agent_port -> cdp :$$sp_port"; \
	done
	@sleep 2
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool

# Grouper + minnows — Chrome solves CF, minnows do bulk. For CF-protected sites.
school-cf: build stop
	@rm -f shoal-pool.json
	@rm -rf /tmp/shoal-chrome-*
	@echo "formation: grouper + $(COUNT) minnows (chrome + tls-client)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@bin/agent -addr :8181 \
		-controller http://localhost:$(CONTROLLER_PORT) \
		-backend chrome -lightpanda-port 9333 \
		> /dev/null 2>&1 & echo "  grouper on :8181 -> chrome :9333"
	@for i in $$(seq 0 $$(($(COUNT) - 1))); do \
		port=$$((8190 + $$i)); \
		bin/agent -addr :$$port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend tls-client \
			> /dev/null 2>&1 & \
		echo "  minnow on :$$port"; \
	done
	@sleep 8
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool

# Mixed school — Lightpanda + minnows. For sites needing JS but no CF.
school-mixed: build stop
	@rm -f shoal-pool.json
	@echo "formation: mixed school (2 lightpanda + $(COUNT) minnows)"
	@bin/controller -addr :$(CONTROLLER_PORT) > /dev/null 2>&1 & echo "  controller on :$(CONTROLLER_PORT)"
	@sleep 0.3
	@for i in 0 1; do \
		agent_port=$$((8181 + $$i)); \
		lp_port=$$((9222 + $$i)); \
		bin/agent -addr :$$agent_port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend lightpanda \
			-lightpanda-bin $(LP_BIN) \
			-lightpanda-port $$lp_port \
			> /dev/null 2>&1 & \
		echo "  redfish :$$agent_port -> lightpanda :$$lp_port"; \
	done
	@for i in $$(seq 0 $$(($(COUNT) - 1))); do \
		port=$$((8190 + $$i)); \
		bin/agent -addr :$$port \
			-controller http://localhost:$(CONTROLLER_PORT) \
			-backend tls-client \
			> /dev/null 2>&1 & \
		echo "  minnow on :$$port"; \
	done
	@sleep 3
	@curl -s localhost:$(CONTROLLER_PORT)/pool/status | python3 -m json.tool

# Legacy aliases
run: school-lp
run-cf: school-cf

# Testsite (for login tests)
testsite: build
	@bin/testsite -addr :$(TESTSITE_PORT) > /dev/null 2>&1 & echo "testsite on :$(TESTSITE_PORT)"

stop:
	@fuser -k $(CONTROLLER_PORT)/tcp 8181/tcp 9333/tcp $(TESTSITE_PORT)/tcp \
		$$(seq -s '/tcp ' 8182 8200)/tcp \
		$$(seq -s '/tcp ' 9222 9230)/tcp \
		2>/dev/null || true
	@sleep 0.3
	@echo "stopped"

clean: stop
	rm -rf bin/ /tmp/shoal-chrome-* shoal-pool.json
