VERSION := 1.0.1
BINARY := firebell
BUILD_DIR := bin
INSTALL_DIR := $(HOME)/.firebell/bin
GO_CMD := go

.PHONY: build install uninstall clean test

build:
	@echo "Building $(BINARY) v$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO_CMD) build -ldflags "-X firebell/internal/config.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/firebell
	@echo "Built: $(BUILD_DIR)/$(BINARY) v$(VERSION)"

test:
	@echo "Running tests..."
	$(GO_CMD) test ./internal/...

install: build
	@echo "Installing to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	@cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@chmod +x $(INSTALL_DIR)/$(BINARY)
	@echo "Done! Add to PATH: export PATH=~/.firebell/bin:\$$PATH"

uninstall:
	@rm -f $(INSTALL_DIR)/$(BINARY)

clean:
	@rm -rf $(BUILD_DIR)
