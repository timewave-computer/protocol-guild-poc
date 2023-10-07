# protocol guild poc

## about

public goods on cosmos are **not** sufficiently funded.

this poc aims to solve that.

inspired by [protocol guild aimed at Ethereum contributors](https://protocol-guild.readthedocs.io/en/latest/index.html).

by utilizing ICAs and IBC, this set of smart contracts enables contributors of all types to be funded across the interchain.

consider an active cosmwasm developer that wishes to be funded. by "registering" with the protocol guild on neutron, it gains exposure for potential funding from:

- protocols/chains
- DAOs
- various foundations
- and more?

we hope that having such options will make our ecosystem to reach a point where entities _not_ contributing for public goods are rare and unusual. not the other way around.

## running the tests

to run the tests that show protocol guild in action, run
```sh
just simtest
```

this will:

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
