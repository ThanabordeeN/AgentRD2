# RDR2 Living NPC Agent - developer shortcuts.
#
# Everything here is a thin wrapper over install.py and the existing scripts,
# so `make` is a convenience, never a requirement.  On Windows use
# `install.ps1` or run the underlying commands directly.

PYTHON ?= python3
SCENARIOS ?= scenarios

.PHONY: help setup doctor demo runtime llm test scenarios lint bridge asi sdk wiki clean

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

setup: ## install + verify (no network, no optional packages)
	$(PYTHON) install.py --yes

doctor: ## diagnose the current setup without changing anything
	$(PYTHON) install.py --check

demo: ## offline end-to-end demo (no API key needed)
	$(PYTHON) run_demo.py

runtime: ## start the runtime with the deterministic rule backend
	$(PYTHON) -m runtime.main --backend rule

llm: ## start the runtime with the built-in live model backend
	$(PYTHON) -m runtime.main --backend llm

test: ## run the full unit + integration test suite
	$(PYTHON) -m unittest discover -s tests -v

scenarios: ## run the deterministic scenario suite
	$(PYTHON) scenario_runner.py --scenarios $(SCENARIOS)

live-scenarios: ## run the live-LLM scenario suite (needs an API key)
	$(PYTHON) scenario_runner.py --backend llm --scenarios scenarios_live

bench: ## measure runtime + action dispatch latency
	$(PYTHON) benchmark_latency.py

sdk: ## download the official ScriptHookRDR2 SDK
	$(PYTHON) install.py --yes --with-sdk

bridge: ## build the portable bridge core + its test
	$(PYTHON) install.py --yes --with-bridge

asi: ## build the in-game .asi (needs CMake + MSVC on Windows)
	$(PYTHON) install.py --yes --with-asi

wiki: ## refresh the offline Red Dead Wiki context packs
	$(PYTHON) scripts/fetch_wiki_context.py
	$(PYTHON) scripts/fetch_character_context.py

clean: ## remove build output and caches (keeps timelines and .env)
	rm -rf bridge/build .pytest_cache
	find . -name __pycache__ -type d -prune -exec rm -rf {} +
