package ibc_test

type ProtocolGuildSplitterInstantiateMsg struct {
	Splits        []DenomSplit `json:"splits"`
	FallbackSplit *SplitType   `json:"fallback_split,omitempty"`
}

type IbcForwarderInstantiateMsg struct {
	NextContract            string `json:"next_contract"`
	RemoteChainConnectionId string `json:"remote_chain_connection_id"`
	RemoteChainChannelId    string `json:"remote_chain_channel_id"`
	Denom                   string `json:"denom"`
	IbcTransferTimeout      string `json:"ibc_transfer_timeout"`
	IcaTimeout              string `json:"ica_timeout"`
}

type Receiver struct {
	Address string `json:"addr"`
	Share   string `json:"share"`
}

type SplitConfig struct {
	Receivers []Receiver `json:"receivers"`
}

type SplitType struct {
	Custom SplitConfig `json:"custom"`
}

type DenomSplit struct {
	Denom string    `json:"denom"`
	Type  SplitType `json:"split"`
}

// queries
type DepositAddress struct{}
type DepositAddressQuery struct {
	DepositAddress DepositAddress `json:"deposit_address"`
}

type QueryResponse struct {
	Data string `json:"data"`
}
