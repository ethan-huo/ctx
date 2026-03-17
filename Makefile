.PHONY: build install clean

BIN_DIR := bin
BIN := $(BIN_DIR)/ctx
LOCAL_BIN := $(HOME)/.local/bin/ctx

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) .

install: build
	@if echo "$$PATH" | tr ':' '\n' | grep -qx "$$(cd $(BIN_DIR) && pwd)"; then \
		echo "$(BIN_DIR)/ is in PATH, no symlink needed"; \
	else \
		ln -sf "$$(cd $(BIN_DIR) && pwd)/ctx" $(LOCAL_BIN); \
		echo "linked → $(LOCAL_BIN)"; \
	fi

clean:
	rm -rf $(BIN_DIR)
