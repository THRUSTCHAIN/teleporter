package tests

import (
	"context"
	"math/big"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	"github.com/ava-labs/teleporter/tests/network"
	"github.com/ava-labs/teleporter/tests/utils"
	localUtils "github.com/ava-labs/teleporter/tests/utils/local-network-utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	. "github.com/onsi/gomega"
)

func SendSpecificReceipts(network network.Network) {
	subnets := network.GetSubnetsInfo()
	Expect(len(subnets)).Should(BeNumerically(">=", 2))
	subnetAInfo := subnets[0]
	subnetBInfo := subnets[1]
	teleporterContractAddress := network.GetTeleporterContractAddress()
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	ctx := context.Background()

	// Use mock token as the fee token
	mockTokenAddress, mockToken := localUtils.DeployExampleERC20(
		context.Background(), fundedKey, subnetAInfo,
	)
	localUtils.ExampleERC20Approve(
		ctx,
		mockToken,
		teleporterContractAddress,
		big.NewInt(0).Mul(big.NewInt(1e18),
			big.NewInt(10)),
		subnetAInfo,
		fundedKey,
	)

	relayerFeePerMessage := big.NewInt(5)
	totalAccumulatedRelayerFee := big.NewInt(10)

	destinationKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	destinationAddress := crypto.PubkeyToAddress(destinationKey.PublicKey)

	// Send two messages from Subnet A to Subnet B
	sendCrossChainMessageInput := teleportermessenger.TeleporterMessageInput{
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		DestinationAddress:      destinationAddress,
		FeeInfo: teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: mockTokenAddress,
			Amount:          relayerFeePerMessage,
		},
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Message:                 []byte{1, 2, 3, 4},
	}

	// Send first message from Subnet A to Subnet B with fee amount 5
	sendCrossChainMsgReceipt, messageID1 := utils.SendCrossChainMessageAndWaitForAcceptance(
		ctx, subnetAInfo, subnetBInfo, sendCrossChainMessageInput, fundedKey)

	// Relay message from SubnetA to SubnetB
	network.RelayMessage(ctx, sendCrossChainMsgReceipt, subnetAInfo, subnetBInfo, true)
	// Check messge delivered
	delivered, err :=
		subnetBInfo.TeleporterMessenger.MessageReceived(&bind.CallOpts{}, subnetAInfo.BlockchainID, messageID1)
	Expect(err).Should(BeNil())
	Expect(delivered).Should(BeTrue())

	// Send second message from Subnet A to Subnet B with fee amount 5
	sendCrossChainMsgReceipt, messageID2 := utils.SendCrossChainMessageAndWaitForAcceptance(
		ctx, subnetAInfo, subnetBInfo, sendCrossChainMessageInput, fundedKey)

	// Relay message from SubnetA to SubnetB
	network.RelayMessage(ctx, sendCrossChainMsgReceipt, subnetAInfo, subnetBInfo, true)
	// Check delivered
	delivered, err =
		subnetBInfo.TeleporterMessenger.MessageReceived(&bind.CallOpts{}, subnetAInfo.BlockchainID, messageID2)
	Expect(err).Should(BeNil())
	Expect(delivered).Should(BeTrue())

	// Relayer send specific receipts to get reward of relaying two messages
	receipt, messageID := utils.SendSpecifiedReceiptsAndWaitForAcceptance(
		ctx,
		subnetAInfo.BlockchainID,
		subnetBInfo,
		[]*big.Int{messageID1, messageID2},
		teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: mockTokenAddress,
			Amount:          big.NewInt(0),
		},
		[]common.Address{},
		fundedKey,
	)

	// Relay message from Subnet B to Subnet A
	network.RelayMessage(ctx, receipt, subnetBInfo, subnetAInfo, true)
	// Check delivered
	delivered, err = subnetAInfo.TeleporterMessenger.MessageReceived(&bind.CallOpts{}, subnetBInfo.BlockchainID, messageID)
	Expect(err).Should(BeNil())
	Expect(delivered).Should(BeTrue())

	// Check the reward amount. The reward amount should be 10
	amount, err :=
		subnetAInfo.TeleporterMessenger.CheckRelayerRewardAmount(&bind.CallOpts{}, fundedAddress, mockTokenAddress)
	Expect(err).Should(BeNil())
	Expect(amount).Should(Equal(totalAccumulatedRelayerFee))

	// Send message from Subnet B to Subnet A to trigger the "regular" method of delivering receipts.
	// The next message from B->A will contain the same receipts that were manually sent in the above steps,
	// but they should not be processed again on Subnet A.
	sendCrossChainMessageInput = teleportermessenger.TeleporterMessageInput{
		DestinationBlockchainID: subnetAInfo.BlockchainID,
		DestinationAddress:      destinationAddress,
		FeeInfo: teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: mockTokenAddress,
			Amount:          big.NewInt(0),
		},
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Message:                 []byte{1, 2, 3, 4},
	}

	// This message will also have the same receipt as the previous message
	receipt, messageID = utils.SendCrossChainMessageAndWaitForAcceptance(
		ctx, subnetBInfo, subnetAInfo, sendCrossChainMessageInput, fundedKey)

	// Relay message from Subnet B to Subnet A
	receipt = network.RelayMessage(ctx, receipt, subnetBInfo, subnetAInfo, true)
	// Check delivered
	delivered, err = subnetAInfo.TeleporterMessenger.MessageReceived(&bind.CallOpts{}, subnetBInfo.BlockchainID, messageID)
	Expect(err).Should(BeNil())
	Expect(delivered).Should(BeTrue())
	// Get the Teleporter message from receive event and confirm that the receipts are delivered again
	receiveEvent, err :=
		utils.GetEventFromLogs(receipt.Logs, subnetAInfo.TeleporterMessenger.ParseReceiveCrossChainMessage)
	Expect(err).Should(BeNil())
	Expect(receiveEvent.Message.Receipts[0].ReceivedMessageID).Should(Equal(messageID1))
	Expect(receiveEvent.Message.Receipts[1].ReceivedMessageID).Should(Equal(messageID2))

	// Check the reward amount remains the same
	amount, err =
		subnetAInfo.TeleporterMessenger.CheckRelayerRewardAmount(&bind.CallOpts{}, fundedAddress, mockTokenAddress)
	Expect(err).Should(BeNil())
	Expect(amount).Should(Equal(totalAccumulatedRelayerFee))
}