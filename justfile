build:
    cargo build

lint:
	cargo +nightly clippy --all-targets -- -D warnings && cargo +nightly fmt --all --check

optimize:
    ./optimize.sh

simtest: optimize
    if [[ $(uname -m) =~ "arm64" ]]; then \
        mv ./artifacts/ibc_forwarder-aarch64.wasm ./artifacts/ibc_forwarder.wasm && \
        mv ./artifacts/protocol_guild_splitter-aarch64.wasm ./artifacts/protocol_guild_splitter.wasm \
    ;fi

    mkdir -p interchaintest/wasms

    cp -R ./artifacts/*.wasm interchaintest/wasms

    go clean -testcache
    cd interchaintest/ && go test -timeout 30m -v ./...

    rm -r interchaintest/wasms

