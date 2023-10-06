#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    to_binary, Binary, CosmosMsg, Deps, DepsMut, Env, MessageInfo, Order, Response, StdError,
    StdResult,
};
use cw2::set_contract_version;

use crate::error::ContractError;
use crate::msg::{ExecuteMsg, InstantiateMsg, QueryMsg, SplitConfig, SplitType};
use crate::state::{FALLBACK_SPLIT, SPLIT_CONFIG_MAP};

const CONTRACT_NAME: &str = "crates.io:protocol-guild-splitter";
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    deps.api.debug("WASMDEBUG: instantiate");
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;

    // we validate the splits and store them per-denom in a map
    for denom_split in msg.splits {
        let validated_split = match denom_split.split {
            SplitType::Custom(c) => c.validate()?,
        };
        SPLIT_CONFIG_MAP.save(deps.storage, denom_split.denom, &validated_split)?;
    }

    // if a fallback split is provided we validate and store it
    if let Some(split) = msg.fallback_split {
        FALLBACK_SPLIT.save(deps.storage, &split.get_split_config()?.validate()?)?;
    }

    Ok(Response::default()
        .add_attribute("method", "protocol_guild_splitter_instantiate"))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    deps.api
        .debug(format!("WASMDEBUG: execute: received msg: {msg:?}").as_str());

    match msg {
        ExecuteMsg::Tick {} => try_distribute(deps, env),
    }
}

pub fn try_distribute(deps: DepsMut, env: Env) -> Result<Response, ContractError> {
    // first we query the contract balances
    let mut balances = deps.querier.query_all_balances(env.contract.address)?;
    let mut distribution_messages: Vec<CosmosMsg> = vec![];

    // then we iterate over our split config and try to match the entries to available balances
    for entry in SPLIT_CONFIG_MAP.range(deps.storage, None, None, Order::Ascending) {
        let (denom, config) = entry?;

        // we try to find the index of matching coin in available balances
        let balances_index = balances.iter().position(|coin| coin.denom == denom);
        if let Some(index) = balances_index {
            // pop the relevant coin and build the transfer messages
            let coin = balances.remove(index);
            let mut transfer_messages =
                config.get_transfer_messages(coin.amount, coin.denom.to_string())?;
            distribution_messages.append(&mut transfer_messages);
        }
    }

    // by now all explicitly defined denom splits have been removed from the
    // balances vector so we can take the remaining balances and distribute
    // them according to the fallback split (if provided)
    // if its not provided, this contract will continue to hold those tokens
    // until its provided
    if let Some(split) = FALLBACK_SPLIT.may_load(deps.storage)? {
        // get the distribution messages and add them to the list
        for leftover_bal in balances {
            let mut fallback_messages =
                split.get_transfer_messages(leftover_bal.amount, leftover_bal.denom)?;
            distribution_messages.append(&mut fallback_messages);
        }
    }

    Ok(Response::default()
        .add_attribute("method", "try_distribute")
        .add_messages(distribution_messages))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::DenomSplit { denom } => Ok(to_binary(&query_split(deps, denom)?)?),
        QueryMsg::Splits {} => Ok(to_binary(&query_all_splits(deps)?)?),
        QueryMsg::FallbackSplit {} => Ok(to_binary(&FALLBACK_SPLIT.may_load(deps.storage)?)?),
        QueryMsg::DepositAddress {} => Ok(to_binary(&Some(env.contract.address))?),
    }
}

pub fn query_all_splits(deps: Deps) -> Result<Vec<(String, SplitConfig)>, StdError> {
    let mut splits: Vec<(String, SplitConfig)> = vec![];

    for entry in SPLIT_CONFIG_MAP.range(deps.storage, None, None, Order::Ascending) {
        let (denom, config) = entry?;
        splits.push((denom, config));
    }

    Ok(splits)
}

pub fn query_split(deps: Deps, denom: String) -> Result<SplitConfig, StdError> {
    for entry in SPLIT_CONFIG_MAP.range(deps.storage, None, None, Order::Ascending) {
        let (entry_denom, config) = entry?;
        if entry_denom == denom {
            return Ok(config);
        }
    }

    Ok(SplitConfig { receivers: vec![] })
}
