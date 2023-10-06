build:
    cargo build

lint:
	cargo +nightly clippy --all-targets -- -D warnings && cargo +nightly fmt --all --check

optimize:
    ./optimize.sh
