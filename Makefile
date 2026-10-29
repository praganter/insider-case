# Runs inside Docker so no local Go/Node toolchain is needed.
.PHONY: test up
test:
	docker build --target test --progress=plain backend

up:
	docker compose up --build
