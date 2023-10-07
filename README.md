# Interchain Protocol Guild - Proof of Concept

## About

**Public goods funding in Cosmos is broken.**

This project, inspired by [protocol guild aimed at Ethereum contributors](https://protocol-guild.readthedocs.io/en/latest/index.html), aims to improve public good funding in the Cosmos ecosystem by:
- Curating and increasing discoverability of public goods efforts
- Making interchain payments to public goods projects simple and convenient
- Instilling a culture of solidarity to support the unseen and underfunded labour in the ecosystem

We are a project built on Neutron. By utilizing IBC and Interchain Accounts, this set of smart contracts enables contributors of all types to be funded across the interchain.

Consider an active cosmwasm developer that wishes to be funded. By "registering" with the protocol guild on neutron, they may receive funding from:
- protocols/chains
- DAOs
- various foundations
- philanthropic whales
- and more?

In the depths of the bear market, we need strong support of our public goods. 

## Running tests

To run the tests that show protocol guild in action, run
```sh
just simtest
```

This will:

- spin up a local interchain
- set up ICS connection between hub and neutron
- set up IBC connections between osmosis, hub, and neutron
- deploy `ibc_forwarder.wasm` and `protocol_guild_splitter.wasm` on neutron
- instantiate the splitter with the following configuration:
  - **osmo**: [(_20% to cosmwasm maintainer_), (_70% to sdk dev team_), (_10% for bug bounties_)]
  - **atom**: [(_50% to sdk dev team_), (_10% for discord ops team_), (_40% for docs upkeep_)]
- instantiate the ibc forwarders and advance their state machines until respective ICAs are created on both the hub and osmosis
- then the flow begins:
    1. a flow of native remote chain tokens starts dripping into our forwarder ICAs
    1. forwarders are ticked, IBC-sending the tokens to the protocol guild splitter contract
    1. protocol guild splitter is ticked, distributing the funds according to the split on a per-denom basis

in this test we perform 4 drips of 10 tokens from osmosis, and 8 drips of 10 tokens from the hub.
means there are **40 osmo** and **80 atom** to distribute. given our split configuration, we expect:
- _cosmwasm maintainer_ to have ( 8 _uosmo_, 0 _uatom_)
- _sdk dev team_ to have (28  _uosmo_, 40 _uatom_)
- _bug bounty_ to have (4 _uosmo_, 0 _uatom_)
- _discord ops team_ to have (0 _uosmo_, 8 _uatom_)
- _docs upkeep_ to have (0 _uosmo_, 32 _uatom_)
