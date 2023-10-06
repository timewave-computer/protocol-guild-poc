use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::{BankMsg, Coin, CosmosMsg, Uint128};

use crate::error::ContractError;

#[cw_serde]
pub struct InstantiateMsg {
    /// list of (denom, split) configurations
    pub splits: Vec<DenomSplit>,
    /// a split for all denoms that are not covered in the
    /// regular `splits` list
    pub fallback_split: Option<SplitType>,
}

#[cw_serde]
pub struct DenomSplit {
    pub denom: String,
    pub split: SplitType,
}

#[cw_serde]
pub enum ExecuteMsg {
    Tick {},
}

#[cw_serde]
pub enum SplitType {
    Custom(SplitConfig),
}

impl SplitType {
    pub fn get_split_config(self) -> Result<SplitConfig, ContractError> {
        match self {
            SplitType::Custom(c) => Ok(c),
        }
    }
}

#[cw_serde]
pub struct SplitConfig {
    pub receivers: Vec<Receiver>,
}

#[cw_serde]
pub struct Receiver {
    pub addr: String,
    pub share: Uint128,
}

impl SplitConfig {
    pub fn validate(self) -> Result<SplitConfig, ContractError> {
        let total_share: Uint128 = self.receivers.iter().map(|r| r.share).sum();

        if total_share == Uint128::new(100) {
            Ok(self)
        } else {
            Err(ContractError::SplitMisconfig {})
        }
    }

    pub fn get_transfer_messages(
        &self,
        amount: Uint128,
        denom: String,
    ) -> Result<Vec<CosmosMsg>, ContractError> {
        let mut msgs: Vec<CosmosMsg> = vec![];

        for receiver in self.receivers.iter() {
            let entitlement = amount
                .checked_multiply_ratio(receiver.share, Uint128::new(100))
                .map_err(|_| ContractError::SplitMisconfig {})?;

            let amount = Coin {
                denom: denom.to_string(),
                amount: entitlement,
            };

            msgs.push(CosmosMsg::Bank(BankMsg::Send {
                to_address: receiver.addr.to_string(),
                amount: vec![amount],
            }));
        }
        Ok(msgs)
    }
}

#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(SplitConfig)]
    DenomSplit { denom: String },
    #[returns(Vec<(String, SplitConfig)>)]
    Splits {},
    #[returns(SplitConfig)]
    FallbackSplit {},
    #[returns(String)]
    DepositAddress {},
}
