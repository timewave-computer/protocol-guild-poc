package ibc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	ibctest "github.com/strangelove-ventures/interchaintest/v4"
	"github.com/strangelove-ventures/interchaintest/v4/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v4/ibc"
	"github.com/strangelove-ventures/interchaintest/v4/relayer"
	"github.com/strangelove-ventures/interchaintest/v4/relayer/rly"
	"github.com/strangelove-ventures/interchaintest/v4/testreporter"
	"github.com/strangelove-ventures/interchaintest/v4/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const gaiaNeutronICSPath = "gn-ics-path"
const gaiaNeutronIBCPath = "gn-ibc-path"
const gaiaOsmosisIBCPath = "go-ibc-path"
const neutronOsmosisIBCPath = "no-ibc-path"
const nativeAtomDenom = "uatom"
const nativeOsmoDenom = "uosmo"
const nativeNtrnDenom = "untrn"

var splitterAddress string
var neutronAtomIbcDenom, neutronOsmoIbcDenom, osmoNeutronAtomIbcDenom, gaiaNeutronOsmoIbcDenom string
var atomNeutronICSConnectionId, neutronAtomICSConnectionId string
var neutronOsmosisIBCConnId, osmosisNeutronIBCConnId string
var atomNeutronIBCConnId, neutronAtomIBCConnId string
var gaiaOsmosisIBCConnId, osmosisGaiaIBCConnId string

func TestProtocolGuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.Background()

	// Modify the the timeout_commit in the config.toml node files
	// to reduce the block commit times. This speeds up the tests
	// by about 35%
	configFileOverrides := make(map[string]any)
	configTomlOverrides := make(testutil.Toml)
	consensus := make(testutil.Toml)
	consensus["timeout_commit"] = "1s"
	configTomlOverrides["consensus"] = consensus
	configFileOverrides["config/config.toml"] = configTomlOverrides

	// Chain Factory
	cf := ibctest.NewBuiltinChainFactory(zaptest.NewLogger(t, zaptest.Level(zap.WarnLevel)), []*ibctest.ChainSpec{
		{Name: "gaia", Version: "v9.1.0", ChainConfig: ibc.ChainConfig{
			GasAdjustment:       1.3,
			GasPrices:           "0.0atom",
			ModifyGenesis:       setupGaiaGenesis(getDefaultInterchainGenesisMessages()),
			ConfigFileOverrides: configFileOverrides,
		}},
		{
			ChainConfig: ibc.ChainConfig{
				Type:    "cosmos",
				Name:    "neutron",
				ChainID: "neutron-2",
				Images: []ibc.DockerImage{
					{
						Repository: "ghcr.io/strangelove-ventures/heighliner/neutron",
						Version:    "v1.0.2",
						UidGid:     "1025:1025",
					},
				},
				Bin:            "neutrond",
				Bech32Prefix:   "neutron",
				Denom:          nativeNtrnDenom,
				GasPrices:      "0.0untrn,0.0uatom",
				GasAdjustment:  1.3,
				TrustingPeriod: "1197504s",
				NoHostMount:    false,
				ModifyGenesis: setupNeutronGenesis(
					"0.05",
					[]string{nativeNtrnDenom},
					[]string{nativeAtomDenom},
					getDefaultInterchainGenesisMessages(),
				),
				ConfigFileOverrides: configFileOverrides,
			},
		},
		{
			Name:    "osmosis",
			Version: "v14.0.0",
			ChainConfig: ibc.ChainConfig{
				Type:         "cosmos",
				Bin:          "osmosisd",
				Bech32Prefix: "osmo",
				Denom:        nativeOsmoDenom,
				ModifyGenesis: setupOsmoGenesis(
					append(getDefaultInterchainGenesisMessages(), "/ibc.applications.interchain_accounts.v1.InterchainAccount"),
				),
				GasPrices:     "0.0uosmo",
				GasAdjustment: 1.3,
				Images: []ibc.DockerImage{
					{
						Repository: "ghcr.io/strangelove-ventures/heighliner/osmosis",
						Version:    "v14.0.0",
						UidGid:     "1025:1025",
					},
				},
				TrustingPeriod:      "336h",
				NoHostMount:         false,
				ConfigFileOverrides: configFileOverrides,
			},
		},
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)

	// We have three chains
	atom, neutron, osmosis := chains[0], chains[1], chains[2]
	cosmosAtom, cosmosNeutron, cosmosOsmosis := atom.(*cosmos.CosmosChain), neutron.(*cosmos.CosmosChain), osmosis.(*cosmos.CosmosChain)

	// Relayer Factory
	client, network := ibctest.DockerSetup(t)
	r := ibctest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel)),
		relayer.CustomDockerImage("ghcr.io/cosmos/relayer", "v2.3.1", rly.RlyDefaultUidGid),
		relayer.RelayerOptionExtraStartFlags{Flags: []string{"-p", "events", "-b", "100", "-d", "--log-format", "console"}},
	).Build(t, client, network)

	// Prep Interchain
	ic := ibctest.NewInterchain().
		AddChain(cosmosAtom).
		AddChain(cosmosNeutron).
		AddChain(cosmosOsmosis).
		AddRelayer(r, "relayer").
		AddProviderConsumerLink(ibctest.ProviderConsumerLink{
			Provider: cosmosAtom,
			Consumer: cosmosNeutron,
			Relayer:  r,
			Path:     gaiaNeutronICSPath,
		}).
		AddLink(ibctest.InterchainLink{
			Chain1:  cosmosAtom,
			Chain2:  cosmosNeutron,
			Relayer: r,
			Path:    gaiaNeutronIBCPath,
		}).
		AddLink(ibctest.InterchainLink{
			Chain1:  cosmosNeutron,
			Chain2:  cosmosOsmosis,
			Relayer: r,
			Path:    neutronOsmosisIBCPath,
		}).
		AddLink(ibctest.InterchainLink{
			Chain1:  cosmosAtom,
			Chain2:  cosmosOsmosis,
			Relayer: r,
			Path:    gaiaOsmosisIBCPath,
		})

	// Log location
	f, err := ibctest.CreateLogFile(fmt.Sprintf("%d.json", time.Now().Unix()))
	require.NoError(t, err)
	// Reporter/logs
	rep := testreporter.NewReporter(f)
	eRep := rep.RelayerExecReporter(t)

	// Build interchain
	require.NoError(
		t,
		ic.Build(ctx, eRep, ibctest.InterchainBuildOptions{
			TestName:          t.Name(),
			Client:            client,
			NetworkID:         network,
			BlockDatabaseFile: ibctest.DefaultBlockDatabaseFilepath(),
			SkipPathCreation:  true,
		}),
		"failed to build interchain")

	err = testutil.WaitForBlocks(ctx, 10, atom, neutron, osmosis)
	require.NoError(t, err, "failed to wait for blocks")

	testCtx := &TestContext{
		OsmoClients:               []*ibc.ClientOutput{},
		GaiaClients:               []*ibc.ClientOutput{},
		NeutronClients:            []*ibc.ClientOutput{},
		OsmoConnections:           []*ibc.ConnectionOutput{},
		GaiaConnections:           []*ibc.ConnectionOutput{},
		NeutronConnections:        []*ibc.ConnectionOutput{},
		NeutronTransferChannelIds: make(map[string]string),
		GaiaTransferChannelIds:    make(map[string]string),
		OsmoTransferChannelIds:    make(map[string]string),
		GaiaIcsChannelIds:         make(map[string]string),
		NeutronIcsChannelIds:      make(map[string]string),
	}

	t.Run("generate IBC paths", func(t *testing.T) {
		generatePath(t, ctx, r, eRep, cosmosAtom.Config().ChainID, cosmosNeutron.Config().ChainID, gaiaNeutronIBCPath)
		generatePath(t, ctx, r, eRep, cosmosAtom.Config().ChainID, cosmosOsmosis.Config().ChainID, gaiaOsmosisIBCPath)
		generatePath(t, ctx, r, eRep, cosmosNeutron.Config().ChainID, cosmosOsmosis.Config().ChainID, neutronOsmosisIBCPath)
		generatePath(t, ctx, r, eRep, cosmosNeutron.Config().ChainID, cosmosAtom.Config().ChainID, gaiaNeutronICSPath)
	})

	t.Run("setup neutron-gaia ICS", func(t *testing.T) {
		generateClient(t, ctx, testCtx, r, eRep, gaiaNeutronICSPath, cosmosAtom, cosmosNeutron)
		neutronClients := testCtx.getChainClients(cosmosNeutron.Config().Name)
		atomClients := testCtx.getChainClients(cosmosAtom.Config().Name)

		err = r.UpdatePath(ctx, eRep, gaiaNeutronICSPath, ibc.PathUpdateOptions{
			SrcClientID: &neutronClients[0].ClientID,
			DstClientID: &atomClients[0].ClientID,
		})
		require.NoError(t, err)

		atomNeutronICSConnectionId, neutronAtomICSConnectionId = generateConnections(t, ctx, testCtx, r, eRep, gaiaNeutronICSPath, cosmosAtom, cosmosNeutron)

		generateICSChannel(t, ctx, r, eRep, gaiaNeutronICSPath, cosmosAtom, cosmosNeutron)

		createValidator(t, ctx, r, eRep, atom, neutron)
		err = testutil.WaitForBlocks(ctx, 2, atom, neutron, osmosis)
		require.NoError(t, err, "failed to wait for blocks")
	})

	t.Run("setup IBC interchain clients, connections, and links", func(t *testing.T) {
		generateClient(t, ctx, testCtx, r, eRep, neutronOsmosisIBCPath, cosmosNeutron, cosmosOsmosis)
		neutronOsmosisIBCConnId, osmosisNeutronIBCConnId = generateConnections(t, ctx, testCtx, r, eRep, neutronOsmosisIBCPath, cosmosNeutron, cosmosOsmosis)
		linkPath(t, ctx, r, eRep, cosmosNeutron, cosmosOsmosis, neutronOsmosisIBCPath)

		generateClient(t, ctx, testCtx, r, eRep, gaiaOsmosisIBCPath, cosmosAtom, cosmosOsmosis)
		gaiaOsmosisIBCConnId, osmosisGaiaIBCConnId = generateConnections(t, ctx, testCtx, r, eRep, gaiaOsmosisIBCPath, cosmosAtom, cosmosOsmosis)
		linkPath(t, ctx, r, eRep, cosmosAtom, cosmosOsmosis, gaiaOsmosisIBCPath)

		generateClient(t, ctx, testCtx, r, eRep, gaiaNeutronIBCPath, cosmosAtom, cosmosNeutron)
		atomNeutronIBCConnId, neutronAtomIBCConnId = generateConnections(t, ctx, testCtx, r, eRep, gaiaNeutronIBCPath, cosmosAtom, cosmosNeutron)
		linkPath(t, ctx, r, eRep, cosmosAtom, cosmosNeutron, gaiaNeutronIBCPath)
	})

	// Start the relayer and clean it up when the test ends.
	err = r.StartRelayer(ctx, eRep, gaiaNeutronICSPath, gaiaNeutronIBCPath, gaiaOsmosisIBCPath, neutronOsmosisIBCPath)
	require.NoError(t, err, "failed to start relayer with given paths")
	t.Cleanup(func() {
		err = r.StopRelayer(ctx, eRep)
		if err != nil {
			t.Logf("failed to stop relayer: %s", err)
		}
	})

	err = testutil.WaitForBlocks(ctx, 2, atom, neutron, osmosis)
	require.NoError(t, err, "failed to wait for blocks")

	// Once the VSC packet has been relayed, x/bank transfers are
	// enabled on Neutron and we can fund its account.
	// The funds for this are sent from a "faucet" account created
	// by interchaintest in the genesis file.

	// funding users
	users := ibctest.GetAndFundTestUsers(t, ctx, "default", int64(500_000_000_000), atom, neutron, osmosis)

	// protocol guild recipients
	protocolGuildRecipients := ibctest.GetAndFundTestUsers(t, ctx, "default", int64(1), neutron, neutron, neutron, neutron, neutron, neutron)
	sdkDevTeam := protocolGuildRecipients[0]
	cosmwasmMaintainer := protocolGuildRecipients[1]
	bugBounty := protocolGuildRecipients[2]
	docsUpkeep := protocolGuildRecipients[3]
	discordOpsTeam := protocolGuildRecipients[4]

	gaiaUser, neutronUser, osmoUser := users[0], users[1], users[2]

	err = testutil.WaitForBlocks(ctx, 10, atom, neutron, osmosis)
	require.NoError(t, err, "failed to wait for blocks")

	t.Run("determine ibc channels", func(t *testing.T) {
		neutronChannelInfo, _ := r.GetChannels(ctx, eRep, cosmosNeutron.Config().ChainID)
		gaiaChannelInfo, _ := r.GetChannels(ctx, eRep, cosmosAtom.Config().ChainID)
		osmoChannelInfo, _ := r.GetChannels(ctx, eRep, cosmosOsmosis.Config().ChainID)

		// Find all pairwise channels
		getPairwiseTransferChannelIds(testCtx, osmoChannelInfo, neutronChannelInfo, osmosisNeutronIBCConnId, neutronOsmosisIBCConnId, osmosis.Config().Name, neutron.Config().Name)
		getPairwiseTransferChannelIds(testCtx, osmoChannelInfo, gaiaChannelInfo, osmosisGaiaIBCConnId, gaiaOsmosisIBCConnId, osmosis.Config().Name, cosmosAtom.Config().Name)
		getPairwiseTransferChannelIds(testCtx, gaiaChannelInfo, neutronChannelInfo, atomNeutronIBCConnId, neutronAtomIBCConnId, cosmosAtom.Config().Name, neutron.Config().Name)
		getPairwiseCCVChannelIds(testCtx, gaiaChannelInfo, neutronChannelInfo, atomNeutronICSConnectionId, neutronAtomICSConnectionId, cosmosAtom.Config().Name, cosmosNeutron.Config().Name)
	})

	t.Run("determine ibc denoms", func(t *testing.T) {
		// We can determine the ibc denoms of:
		// 1. ATOM on Neutron
		neutronAtomIbcDenom = testCtx.getIbcDenom(
			testCtx.NeutronTransferChannelIds[cosmosAtom.Config().Name],
			nativeAtomDenom,
		)
		// 2. Osmo on neutron
		neutronOsmoIbcDenom = testCtx.getIbcDenom(
			testCtx.NeutronTransferChannelIds[cosmosOsmosis.Config().Name],
			nativeOsmoDenom,
		)
		// 3. hub atom => neutron => osmosis
		osmoNeutronAtomIbcDenom = testCtx.getMultihopIbcDenom(
			[]string{
				testCtx.OsmoTransferChannelIds[cosmosNeutron.Config().Name],
				testCtx.NeutronTransferChannelIds[cosmosAtom.Config().Name],
			},
			nativeAtomDenom,
		)
		// 4. osmosis osmo => neutron => hub
		gaiaNeutronOsmoIbcDenom = testCtx.getMultihopIbcDenom(
			[]string{
				testCtx.GaiaTransferChannelIds[cosmosNeutron.Config().Name],
				testCtx.NeutronTransferChannelIds[cosmosOsmosis.Config().Name],
			},
			nativeOsmoDenom,
		)
	})
	var osmoForwarderAddress, gaiaForwarderAddress string
	var osmoForwarderICA, gaiaForwarderICA string

	t.Run("protocol guild setup", func(t *testing.T) {
		// Wasm code that we need to store on Neutron
		const ibcForwarderContractPath = "./wasms/ibc_forwarder.wasm"
		const splitterContractPath = "./wasms/protocol_guild_splitter.wasm"

		// After storing on Neutron, we will receive a code id
		// We parse all the subcontracts into uint64
		var splitterCodeIdStr string
		var ibcForwarderCodeIdStr string

		t.Run("deploy contracts", func(t *testing.T) {
			// store clock and get code id
			splitterCodeIdStr, err = cosmosNeutron.StoreContract(ctx, neutronUser.KeyName, splitterContractPath)
			require.NoError(t, err, "failed to store splitter contract")

			// store clock and get code id
			ibcForwarderCodeIdStr, err = cosmosNeutron.StoreContract(ctx, neutronUser.KeyName, ibcForwarderContractPath)
			require.NoError(t, err, "failed to store ibc forwarder contract")

			require.NoError(t, testutil.WaitForBlocks(ctx, 5, cosmosNeutron, cosmosAtom, cosmosOsmosis))
		})

		t.Run("instantiate contracts", func(t *testing.T) {

			protocolGuildInstantiateMsg := ProtocolGuildSplitterInstantiateMsg{
				Splits: []DenomSplit{
					{
						Denom: neutronOsmoIbcDenom,
						Type: SplitType{
							Custom: SplitConfig{
								Receivers: []Receiver{
									{
										Address: cosmwasmMaintainer.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "20",
									},
									{
										Address: sdkDevTeam.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "70",
									},
									{
										Address: bugBounty.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "10",
									},
								},
							},
						},
					},
					{
						Denom: neutronAtomIbcDenom,
						Type: SplitType{
							Custom: SplitConfig{
								Receivers: []Receiver{
									{
										Address: sdkDevTeam.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "50",
									},
									{
										Address: docsUpkeep.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "40",
									},
									{
										Address: discordOpsTeam.Bech32Address(cosmosNeutron.Config().Bech32Prefix),
										Share:   "10",
									},
								},
							},
						},
					},
				},
				FallbackSplit: nil,
			}

			str, err := json.Marshal(protocolGuildInstantiateMsg)
			require.NoError(t, err, "Failed to marshall protocolGuildInstantiateMsg")
			instantiateMsg := string(str)

			splitterAddress, err = cosmosNeutron.InstantiateContract(ctx, neutronUser.KeyName, splitterCodeIdStr, instantiateMsg, true)
			require.NoError(t, err)
			require.NoError(t, testutil.WaitForBlocks(ctx, 5, atom, neutron, osmosis))

			println("splitter  address: ", splitterAddress)

			icaTimeout := "100"
			ibcTransferTimeout := "100"

			osmoForwarderInstantiateMsg := IbcForwarderInstantiateMsg{
				NextContract:            splitterAddress,
				RemoteChainConnectionId: neutronOsmosisIBCConnId,
				RemoteChainChannelId:    testCtx.OsmoTransferChannelIds[cosmosNeutron.Config().Name],
				Denom:                   nativeOsmoDenom,
				IbcTransferTimeout:      ibcTransferTimeout,
				IcaTimeout:              icaTimeout,
			}

			str, err = json.Marshal(osmoForwarderInstantiateMsg)
			require.NoError(t, err, "Failed to marshall osmoForwarderInstantiateMsg")
			instantiateMsg = string(str)

			osmoForwarderAddress, err = cosmosNeutron.InstantiateContract(ctx, neutronUser.KeyName, ibcForwarderCodeIdStr, instantiateMsg, true)
			require.NoError(t, err)
			require.NoError(t, testutil.WaitForBlocks(ctx, 5, atom, neutron, osmosis))

			gaiaForwarderInstantiateMsg := IbcForwarderInstantiateMsg{
				NextContract:            splitterAddress,
				RemoteChainConnectionId: neutronAtomIBCConnId,
				RemoteChainChannelId:    testCtx.GaiaTransferChannelIds[cosmosNeutron.Config().Name],
				Denom:                   nativeAtomDenom,
				IbcTransferTimeout:      ibcTransferTimeout,
				IcaTimeout:              icaTimeout,
			}

			str, err = json.Marshal(gaiaForwarderInstantiateMsg)
			require.NoError(t, err, "Failed to marshall gaiaForwarderInstantiateMsg")
			instantiateMsg = string(str)

			gaiaForwarderAddress, err = cosmosNeutron.InstantiateContract(ctx, neutronUser.KeyName, ibcForwarderCodeIdStr, instantiateMsg, true)
			require.NoError(t, err)
			require.NoError(t, testutil.WaitForBlocks(ctx, 5, atom, neutron, osmosis))

			println("gaia forwarder address: ", gaiaForwarderAddress)
			println("osmo forwarder address: ", osmoForwarderAddress)
		})

		t.Run("fund contracts with neutron", func(t *testing.T) {
			require.NoError(t,
				neutron.SendFunds(ctx, neutronUser.KeyName, ibc.WalletAmount{
					Address: gaiaForwarderAddress,
					Amount:  500000,
					Denom:   nativeNtrnDenom,
				}),
				"failed to send funds from neutron user to gaiaForwarderAddress")
			require.NoError(t,
				neutron.SendFunds(ctx, neutronUser.KeyName, ibc.WalletAmount{
					Address: osmoForwarderAddress,
					Amount:  500000,
					Denom:   nativeNtrnDenom,
				}),
				"failed to send funds from neutron user to osmoForwarderAddress")
			require.NoError(t,
				neutron.SendFunds(ctx, neutronUser.KeyName, ibc.WalletAmount{
					Address: splitterAddress,
					Amount:  500000,
					Denom:   nativeNtrnDenom,
				}),
				"failed to send funds from neutron user to splitterAddress")

			bal, err := neutron.GetBalance(ctx, gaiaForwarderAddress, nativeNtrnDenom)
			require.NoError(t, err)
			require.Equal(t, int64(500000), bal)
			bal, err = neutron.GetBalance(ctx, osmoForwarderAddress, nativeNtrnDenom)
			require.NoError(t, err)
			require.Equal(t, int64(500000), bal)
			bal, err = neutron.GetBalance(ctx, splitterAddress, nativeNtrnDenom)
			require.NoError(t, err)
			require.Equal(t, int64(500000), bal)
		})

		queryBalances := func(addr string, label string) {
			neutronOsmoBal, err := cosmosNeutron.GetBalance(ctx, addr, neutronOsmoIbcDenom)
			require.NoError(t, err)
			neutronAtomBal, err := cosmosNeutron.GetBalance(ctx, addr, neutronAtomIbcDenom)
			require.NoError(t, err)

			println(label, " [neutronOsmoBal: ", neutronOsmoBal, ", neutronAtomBal: ", neutronAtomBal, "]")
		}

		tickContract := func(addr string, label string) {
			println("ticking ", label)
			cmd := []string{"neutrond", "tx", "wasm", "execute", addr,
				`{"tick":{}}`,
				"--from", neutronUser.KeyName,
				"--gas-prices", "0.0untrn",
				"--gas-adjustment", `1.8`,
				"--output", "json",
				"--node", neutron.GetRPCAddress(),
				"--home", neutron.HomeDir(),
				"--chain-id", neutron.Config().ChainID,
				"--from", neutronUser.KeyName,
				"--gas", "auto",
				"--keyring-backend", keyring.BackendTest,
				"-y",
			}

			_, _, err := cosmosNeutron.Exec(ctx, cmd, nil)
			require.NoError(t, err)

			err = testutil.WaitForBlocks(ctx, 5, atom, neutron, osmosis)
			require.NoError(t, err, "failed to wait for blocks")
		}

		t.Run("create ICAs for forwarders", func(t *testing.T) {
			tickContract(osmoForwarderAddress, "osmo forwarder")
			tickContract(gaiaForwarderAddress, "gaia forwarder")
			tickContract(osmoForwarderAddress, "osmo forwarder")
			tickContract(gaiaForwarderAddress, "gaia forwarder")
		})

		t.Run("query forwarder deposit addresses", func(t *testing.T) {
			var response QueryResponse

			depositAddressQuery := DepositAddressQuery{
				DepositAddress: DepositAddress{},
			}

			err := cosmosNeutron.QueryContract(ctx, gaiaForwarderAddress, depositAddressQuery, &response)
			require.NoError(t, err, "failed to query gaiaForwarderICA address")
			gaiaForwarderICA = response.Data

			err = cosmosNeutron.QueryContract(ctx, osmoForwarderAddress, depositAddressQuery, &response)
			require.NoError(t, err, "failed to query osmoForwarderICA address")
			osmoForwarderICA = response.Data

			println("gaiaForwarderICA: ", gaiaForwarderICA)
			println("osmoForwarderICA: ", osmoForwarderICA)
		})

		t.Run("fund forwarder ICAs with some denoms", func(t *testing.T) {

			require.NoError(t,
				cosmosOsmosis.SendFunds(ctx, osmoUser.KeyName, ibc.WalletAmount{
					Address: osmoForwarderICA,
					Amount:  500000,
					Denom:   osmosis.Config().Denom,
				}),
				"failed to send funds from osmo user to osmoForwarderICA")

			require.NoError(t,
				cosmosAtom.SendFunds(ctx, gaiaUser.KeyName, ibc.WalletAmount{
					Address: gaiaForwarderICA,
					Amount:  500000,
					Denom:   cosmosAtom.Config().Denom,
				}),
				"failed to send funds from osmo user to gaiaForwarderICA")

			require.NoError(t, testutil.WaitForBlocks(ctx, 5, atom, neutron, osmosis))

			bal, err := cosmosAtom.GetBalance(ctx, gaiaForwarderICA, cosmosAtom.Config().Denom)
			require.NoError(t, err)
			require.Equal(t, int64(500000), bal)

			bal, err = cosmosOsmosis.GetBalance(ctx, osmoForwarderICA, cosmosOsmosis.Config().Denom)
			require.NoError(t, err)
			require.Equal(t, int64(500000), bal)
		})

		queryAllBalances := func() {
			queryBalances(splitterAddress, "splitter")
			queryBalances(sdkDevTeam.Bech32Address(cosmosNeutron.Config().Bech32Prefix), "sdk_dev_team")
			queryBalances(cosmwasmMaintainer.Bech32Address(cosmosNeutron.Config().Bech32Prefix), "cosmwasm_maintainer")
			queryBalances(discordOpsTeam.Bech32Address(cosmosNeutron.Config().Bech32Prefix), "discord_ops_team")
			queryBalances(bugBounty.Bech32Address(cosmosNeutron.Config().Bech32Prefix), "bug_bounty")
			queryBalances(docsUpkeep.Bech32Address(cosmosNeutron.Config().Bech32Prefix), "docs_upkeep")
		}

		t.Run("tick forwarders to forward funds from ica to splitter", func(t *testing.T) {
			println("\ninitial balances: ")
			queryAllBalances()

			println("\ntick forwarders 10x to imitate some continuous stream of funds...")
			for i := 0; i < 4; i++ {
				tickContract(osmoForwarderAddress, "osmo forwarder")
				tickContract(gaiaForwarderAddress, "gaia forwarder")
			}

			queryAllBalances()
			println("\ndistribute the splits...")
			tickContract(splitterAddress, "splitter")

			queryAllBalances()

			println("\ne.g. one chain stops streaming, other continues..")
			for i := 0; i < 4; i++ {
				tickContract(gaiaForwarderAddress, "gaia forwarder")
				tickContract(splitterAddress, "splitter\n")
				queryAllBalances()
			}
		})
	})
}
