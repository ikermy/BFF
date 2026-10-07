.PHONY: help test vet

help:
	@echo "Available targets:"
	@echo "  make test  # run Go unit tests"
	@echo "  make vet   # run go vet"
	@echo ""
	@echo "Local/dev deployments live in the barcode-deploy repo (k8s overlays),"
	@echo "not in this repository."

test:
	go test ./... -count=1

vet:
	go vet ./...
