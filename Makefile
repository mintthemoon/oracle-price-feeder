all: get-just

get-just:
	@echo "❗  make is deprecated in this project  ❗"
	@echo "Please transition to our new build manager, just (https://just.systems)"
	@echo "If you need to install it, check out asdf: https://asdf-vm.com/guide/getting-started.html"
	@echo "  asdf plugin add just"
	@echo "  asdf install just"
	@echo "  just"
	@echo
	@echo "Happy hacking! See the README for details."
	@exit 1

build: get-just

install: get-just

test-unit: get-just

lint: get-just

.PHONY: all get-just build install test-unit lint
