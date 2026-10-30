# Runs inside Docker so no local Go/Node toolchain is needed.
.PHONY: test up hooks
test:
	docker compose run --rm --build test
	docker compose run --rm --build test-frontend

up:
	docker compose up --build

# One-time per clone: enables the session-export leak check on commit.
hooks:
	git config core.hooksPath .githooks
