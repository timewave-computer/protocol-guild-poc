use cosmos_sdk_proto::ibc::applications::transfer::v1::MsgTransfer;
#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    from_binary, to_binary, to_vec, Binary, CosmosMsg, CustomQuery, Deps, DepsMut, Env,
    MessageInfo, Reply, Response, StdError, StdResult, Storage, SubMsg, Uint128,
};
use cw2::set_contract_version;
use neutron_sdk::{
    bindings::{
        msg::{IbcFee, MsgSubmitTxResponse, NeutronMsg},
        query::NeutronQuery,
        types::ProtobufAny,
    },
    interchain_txs::helpers::get_port_id,
    sudo::msg::{RequestPacket, SudoMsg},
    NeutronError, NeutronResult,
};
use prost::Message;

use crate::{
    msg::{
        ContractState, ExecuteMsg, InstantiateMsg, OpenAckVersion, QueryMsg, RemoteChainInfo,
        SudoPayload,
    },
    state::{
        CONTRACT_STATE, INTERCHAIN_ACCOUNTS, NEXT_CONTRACT, REMOTE_CHAIN_INFO, REPLY_ID_STORAGE,
        SUDO_PAYLOAD,
    },
};

const CONTRACT_NAME: &str = "crates.io:ibc-forwarder";
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");
const INTERCHAIN_ACCOUNT_ID: &str = "ica";
pub const SUDO_PAYLOAD_REPLY_ID: u64 = 1;

type QueryDeps<'a> = Deps<'a, NeutronQuery>;
type ExecuteDeps<'a> = DepsMut<'a, NeutronQuery>;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: ExecuteDeps,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> NeutronResult<Response<NeutronMsg>> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;

    let next_contract = deps.api.addr_validate(&msg.next_contract)?;
    NEXT_CONTRACT.save(deps.storage, &next_contract)?;

    let remote_chain_info = RemoteChainInfo {
        connection_id: msg.remote_chain_connection_id,
        channel_id: msg.remote_chain_channel_id,
        denom: msg.denom,
        ibc_fee: IbcFee {
            // must be empty
            recv_fee: vec![],
            ack_fee: vec![cosmwasm_std::Coin {
                denom: "untrn".to_string(),
                amount: Uint128::new(10000),
            }],
            timeout_fee: vec![cosmwasm_std::Coin {
                denom: "untrn".to_string(),
                amount: Uint128::new(10000),
            }],
        },
        ica_timeout: msg.ica_timeout,
        ibc_transfer_timeout: msg.ibc_transfer_timeout,
    };
    REMOTE_CHAIN_INFO.save(deps.storage, &remote_chain_info)?;
    CONTRACT_STATE.save(deps.storage, &ContractState::Instantiated)?;

    Ok(Response::default()
        .add_attribute("method", "ibc_forwarder_instantiate")
        .add_attribute("next_contract", next_contract)
        .add_attribute("contract_state", "instantiated"))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: ExecuteDeps,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> NeutronResult<Response<NeutronMsg>> {
    match msg {
        ExecuteMsg::Tick {} => try_tick(deps, env, info),
    }
}

/// attempts to advance the state machine
fn try_tick(
    deps: ExecuteDeps,
    env: Env,
    _info: MessageInfo,
) -> NeutronResult<Response<NeutronMsg>> {
    let current_state = CONTRACT_STATE.load(deps.storage)?;
    match current_state {
        ContractState::Instantiated => try_register_ica(deps, env),
        ContractState::IcaCreated => try_forward_funds(env, deps),
        ContractState::Complete => {
            Ok(Response::default().add_attribute("contract_state", "completed"))
        }
    }
}

/// tries to register an ICA on the remote chain
fn try_register_ica(deps: ExecuteDeps, env: Env) -> NeutronResult<Response<NeutronMsg>> {
    let remote_chain_info = REMOTE_CHAIN_INFO.load(deps.storage)?;

    let register_msg = NeutronMsg::register_interchain_account(
        remote_chain_info.connection_id,
        INTERCHAIN_ACCOUNT_ID.to_string(),
    );

    let key = get_port_id(env.contract.address.as_str(), INTERCHAIN_ACCOUNT_ID);

    // we are saving empty data here because we handle response of registering ICA in sudo_open_ack method
    INTERCHAIN_ACCOUNTS.save(deps.storage, key, &None)?;

    Ok(Response::new()
        .add_attribute("method", "try_register_ica")
        .add_message(register_msg))
}

fn try_forward_funds(env: Env, mut deps: ExecuteDeps) -> NeutronResult<Response<NeutronMsg>> {
    // first we verify whether the next contract is ready for receiving the funds
    let next_contract = NEXT_CONTRACT.load(deps.storage)?;
    let deposit_address_query: Option<String> = deps
        .querier
        .query_wasm_smart(next_contract, &QueryMsg::DepositAddress {})?;

    // if query returns None, then we error and wait
    let Some(deposit_address) = deposit_address_query else {
        return Err(NeutronError::Std(
            StdError::not_found("Next contract is not ready for receiving the funds yet")
        ))
    };

    let port_id = get_port_id(env.contract.address.as_str(), INTERCHAIN_ACCOUNT_ID);
    let interchain_account = INTERCHAIN_ACCOUNTS.load(deps.storage, port_id.clone())?;

    match interchain_account {
        Some((address, controller_conn_id)) => {
            let remote_chain_info = REMOTE_CHAIN_INFO.load(deps.storage)?;

            let transfer_msg = MsgTransfer {
                source_port: "transfer".to_string(),
                source_channel: remote_chain_info.channel_id,
                token: Some(cosmos_sdk_proto::cosmos::base::v1beta1::Coin {
                    denom: remote_chain_info.denom,
                    amount: "10".to_string(), // TODO: remove hardcoded amount
                }),
                sender: address,
                receiver: deposit_address,
                timeout_height: None,
                timeout_timestamp: env
                    .block
                    .time
                    .plus_seconds(remote_chain_info.ica_timeout.u64())
                    .plus_seconds(remote_chain_info.ibc_transfer_timeout.u64())
                    .nanos(),
            };

            let protobuf_msg = to_proto_msg_transfer(transfer_msg)?;

            // tx to our ICA that wraps the transfer message defined above
            let submit_msg = NeutronMsg::submit_tx(
                controller_conn_id,
                INTERCHAIN_ACCOUNT_ID.to_string(),
                vec![protobuf_msg],
                "".to_string(),
                remote_chain_info.ica_timeout.u64(),
                remote_chain_info.ibc_fee,
            );

            // sudo callback msg
            let submsg = msg_with_sudo_callback(
                deps.branch(),
                submit_msg,
                SudoPayload {
                    port_id,
                    message: "try_forward_funds".to_string(),
                },
            )?;

            Ok(Response::default()
                .add_attribute("method", "try_forward_funds")
                .add_submessage(submsg))
        }
        None => {
            // I can't think of a case of how we could end up here as `sudo_open_ack`
            // callback advances the state to `ICACreated` and stores the ICA.
            // just in case, we revert the state to `Instantiated` to restart the flow.
            CONTRACT_STATE.save(deps.storage, &ContractState::Instantiated)?;
            Ok(Response::default()
                .add_attribute("method", "try_forward_funds")
                .add_attribute("error", "no_ica_found"))
        }
    }
}

pub fn to_proto_msg_transfer(msg: impl Message) -> NeutronResult<ProtobufAny> {
    // Serialize the Transfer message
    let mut buf = Vec::new();
    buf.reserve(msg.encoded_len());
    if let Err(e) = msg.encode(&mut buf) {
        return Err(StdError::generic_err(format!("Encode error: {e}")).into());
    }

    Ok(ProtobufAny {
        type_url: "/ibc.applications.transfer.v1.MsgTransfer".to_string(),
        value: Binary::from(buf),
    })
}

fn msg_with_sudo_callback<C: Into<CosmosMsg<T>>, T>(
    deps: ExecuteDeps,
    msg: C,
    payload: SudoPayload,
) -> StdResult<SubMsg<T>> {
    save_reply_payload(deps.storage, payload)?;
    Ok(SubMsg::reply_on_success(msg, SUDO_PAYLOAD_REPLY_ID))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: QueryDeps, env: Env, msg: QueryMsg) -> NeutronResult<Binary> {
    match msg {
        // we expect to receive funds into our ICA account on the remote chain.
        // if the ICA had not been opened yet, we return `None` so that the
        // contract querying this will be instructed to wait and retry.
        QueryMsg::DepositAddress {} => {
            let key = get_port_id(env.contract.address.as_str(), INTERCHAIN_ACCOUNT_ID);
            // here we want to return None instead of any errors in case no ICA
            // is registered yet
            let ica = match INTERCHAIN_ACCOUNTS.may_load(deps.storage, key)? {
                Some(entry) => {
                    if let Some((addr, _)) = entry {
                        Some(addr)
                    } else {
                        None
                    }
                }
                None => None,
            };

            Ok(to_binary(&ica)?)
        }
        QueryMsg::IcaAddress {} => Ok(to_binary(&get_ica(deps, &env, INTERCHAIN_ACCOUNT_ID)?.0)?),
        QueryMsg::RemoteChainInfo {} => Ok(to_binary(&REMOTE_CHAIN_INFO.may_load(deps.storage)?)?),
        QueryMsg::ContractState {} => Ok(to_binary(&CONTRACT_STATE.may_load(deps.storage)?)?),
    }
}

fn get_ica(
    deps: Deps<impl CustomQuery>,
    env: &Env,
    interchain_account_id: &str,
) -> Result<(String, String), StdError> {
    let key = get_port_id(env.contract.address.as_str(), interchain_account_id);

    INTERCHAIN_ACCOUNTS
        .load(deps.storage, key)?
        .ok_or_else(|| StdError::generic_err("Interchain account is not created yet"))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn sudo(deps: ExecuteDeps, env: Env, msg: SudoMsg) -> StdResult<Response> {
    deps.api
        .debug(format!("WASMDEBUG: sudo: received sudo msg: {msg:?}").as_str());

    match msg {
        // For handling successful (non-error) acknowledgements.
        SudoMsg::Response { request, data } => sudo_response(deps, request, data),

        // For handling error acknowledgements.
        SudoMsg::Error { request, details } => sudo_error(deps, request, details),

        // For handling error timeouts.
        SudoMsg::Timeout { request } => sudo_timeout(deps, env, request),

        // For handling successful registering of ICA
        SudoMsg::OpenAck {
            port_id,
            channel_id,
            counterparty_channel_id,
            counterparty_version,
        } => sudo_open_ack(
            deps,
            env,
            port_id,
            channel_id,
            counterparty_channel_id,
            counterparty_version,
        ),
        _ => Ok(Response::default()),
    }
}

// handler
fn sudo_open_ack(
    deps: ExecuteDeps,
    _env: Env,
    port_id: String,
    _channel_id: String,
    _counterparty_channel_id: String,
    counterparty_version: String,
) -> StdResult<Response> {
    // The version variable contains a JSON value with multiple fields,
    // including the generated account address.
    let parsed_version: Result<OpenAckVersion, _> =
        serde_json_wasm::from_str(counterparty_version.as_str());

    // get the parsed OpenAckVersion or return an error if we fail
    let Ok(parsed_version) = parsed_version else {
        return Err(StdError::generic_err("Can't parse counterparty_version"))
    };

    // Update the storage record associated with the interchain account.
    INTERCHAIN_ACCOUNTS.save(
        deps.storage,
        port_id,
        &Some((
            parsed_version.clone().address,
            parsed_version.controller_connection_id,
        )),
    )?;
    CONTRACT_STATE.save(deps.storage, &ContractState::IcaCreated)?;

    Ok(Response::default().add_attribute("method", "sudo_open_ack"))
}

fn sudo_response(deps: ExecuteDeps, request: RequestPacket, data: Binary) -> StdResult<Response> {
    deps.api
        .debug(format!("WASMDEBUG: sudo_response: sudo received: {request:?} {data:?}").as_str());

    // either of these errors will close the channel
    request
        .sequence
        .ok_or_else(|| StdError::generic_err("sequence not found"))?;

    request
        .source_channel
        .ok_or_else(|| StdError::generic_err("channel_id not found"))?;

    Ok(Response::default().add_attribute("method", "sudo_response"))
}

fn sudo_timeout(deps: ExecuteDeps, _env: Env, request: RequestPacket) -> StdResult<Response> {
    deps.api
        .debug(format!("WASMDEBUG: sudo timeout request: {request:?}").as_str());

    // revert the state to Instantiated to force re-creation of ICA
    CONTRACT_STATE.save(deps.storage, &ContractState::Instantiated)?;

    // returning Ok as this is anticipated. channel is already closed.
    Ok(Response::default())
}

fn sudo_error(deps: ExecuteDeps, request: RequestPacket, details: String) -> StdResult<Response> {
    deps.api
        .debug(format!("WASMDEBUG: sudo error: {details}").as_str());

    deps.api
        .debug(format!("WASMDEBUG: request packet: {request:?}").as_str());

    // either of these errors will close the channel
    request
        .sequence
        .ok_or_else(|| StdError::generic_err("sequence not found"))?;

    request
        .source_channel
        .ok_or_else(|| StdError::generic_err("channel_id not found"))?;

    Ok(Response::default().add_attribute("method", "sudo_error"))
}

pub fn save_reply_payload(store: &mut dyn Storage, payload: SudoPayload) -> StdResult<()> {
    REPLY_ID_STORAGE.save(store, &to_vec(&payload)?)
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn reply(deps: ExecuteDeps, env: Env, msg: Reply) -> StdResult<Response> {
    deps.api
        .debug(format!("WASMDEBUG: reply msg: {msg:?}").as_str());
    match msg.id {
        SUDO_PAYLOAD_REPLY_ID => prepare_sudo_payload(deps, env, msg),
        _ => Err(StdError::generic_err(format!(
            "unsupported reply message id {}",
            msg.id
        ))),
    }
}

fn prepare_sudo_payload(mut deps: ExecuteDeps, _env: Env, msg: Reply) -> StdResult<Response> {
    let payload = read_reply_payload(deps.storage)?;
    let resp: MsgSubmitTxResponse = serde_json_wasm::from_slice(
        msg.result
            .into_result()
            .map_err(StdError::generic_err)?
            .data
            .ok_or_else(|| StdError::generic_err("no result"))?
            .as_slice(),
    )
    .map_err(|e| StdError::generic_err(format!("failed to parse response: {e:?}")))?;
    deps.api
        .debug(format!("WASMDEBUG: reply msg: {resp:?}").as_str());
    let seq_id = resp.sequence_id;
    let channel_id = resp.channel;
    save_sudo_payload(deps.branch().storage, channel_id, seq_id, payload)?;
    Ok(Response::new())
}

pub fn read_reply_payload(store: &mut dyn Storage) -> StdResult<SudoPayload> {
    let data = REPLY_ID_STORAGE.load(store)?;
    from_binary(&Binary(data))
}

pub fn save_sudo_payload(
    store: &mut dyn Storage,
    channel_id: String,
    seq_id: u64,
    payload: SudoPayload,
) -> StdResult<()> {
    SUDO_PAYLOAD.save(store, (channel_id, seq_id), &to_vec(&payload)?)
}
