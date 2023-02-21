package keeper_test

import (
	"github.com/SigmaGmbH/evm-module/x/evm/keeper"
	"github.com/SigmaGmbH/evm-module/x/evm/statedb"
	"github.com/SigmaGmbH/evm-module/x/evm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
)

func (suite *KeeperTestSuite) TestNativeCurrencyTransfer() {
	var (
		err             error
		msg             *types.MsgHandleTx
		signer          ethtypes.Signer
		vmdb            *statedb.StateDB
		chainCfg        *params.ChainConfig
		expectedGasUsed uint64
		transferAmount  int64
	)

	testCases := []struct {
		name     string
		malleate func()
		expErr   bool
	}{
		{
			"Transfer funds tx",
			func() {
				transferAmount = 1000
				msg, _, err = newEthMsgTx(
					vmdb.GetNonce(suite.address),
					suite.ctx.BlockHeight(),
					suite.address,
					chainCfg,
					suite.signer,
					signer,
					ethtypes.AccessListTxType,
					nil,
					nil,
					big.NewInt(transferAmount),
				)
				suite.Require().NoError(err)
				expectedGasUsed = params.TxGas
			},
			false,
		},
		{
			"Exceeding balance transfer tx",
			func() {
				transferAmount = 1000
				wrongAmount := int64(100000)
				msg, _, err = newEthMsgTx(
					vmdb.GetNonce(suite.address),
					suite.ctx.BlockHeight(),
					suite.address,
					chainCfg,
					suite.signer,
					signer,
					ethtypes.AccessListTxType,
					nil,
					nil,
					big.NewInt(wrongAmount),
				)
				suite.Require().NoError(err)
				expectedGasUsed = params.TxGas
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupSGXVMTest()

			keeperParams := suite.app.EvmKeeper.GetParams(suite.ctx)
			chainCfg = keeperParams.ChainConfig.EthereumConfig(suite.app.EvmKeeper.ChainID())
			signer = ethtypes.LatestSignerForChainID(suite.app.EvmKeeper.ChainID())
			vmdb = suite.StateDB()

			tc.malleate()

			err := suite.app.EvmKeeper.SetBalance(suite.ctx, suite.address, big.NewInt(transferAmount))
			suite.Require().NoError(err)

			balanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
			receiverBalanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})

			res, err := suite.app.EvmKeeper.HandleTx(suite.ctx, msg)
			if tc.expErr {
				suite.Require().Equal(res.VmError, "evm error: OutOfFund")
				suite.Require().NoError(err)
				return
			} else {
				// Check sender's balance
				expectedBalance := balanceBefore.Sub(balanceBefore, big.NewInt(transferAmount))
				balanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
				isSenderBalanceCorrect := expectedBalance.Cmp(balanceAfter)
				suite.Require().True(isSenderBalanceCorrect == 0, "Incorrect sender's balance")

				// Check receiver's balance
				receiverBalanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})
				expectedReceiverBalance := receiverBalanceBefore.Add(receiverBalanceBefore, big.NewInt(transferAmount))
				isReceiverBalanceCorrect := expectedReceiverBalance.Cmp(receiverBalanceAfter)
				suite.Require().True(isReceiverBalanceCorrect == 0, "Incorrect receiver's balance")

				suite.Require().NoError(err)
				suite.Require().Equal(expectedGasUsed, res.GasUsed)
				suite.Require().False(res.Failed())
			}
		})
	}
}

func (suite *KeeperTestSuite) TestDryRun() {
	var (
		signer   ethtypes.Signer
		vmdb     *statedb.StateDB
		chainCfg *params.ChainConfig
	)

	amountToTransfer := int64(100)

	testCases := []struct {
		name   string
		commit bool
	}{
		{
			"Transfer in normal mode should update nonce and balance",
			true,
		},
		{
			"Transfer in dry mode should not update nonce and balance",
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupSGXVMTest()

			keeperParams := suite.app.EvmKeeper.GetParams(suite.ctx)
			chainCfg = keeperParams.ChainConfig.EthereumConfig(suite.app.EvmKeeper.ChainID())
			signer = ethtypes.LatestSignerForChainID(suite.app.EvmKeeper.ChainID())
			vmdb = suite.StateDB()

			err := suite.app.EvmKeeper.SetBalance(suite.ctx, suite.address, big.NewInt(amountToTransfer))
			suite.Require().NoError(err)

			cfg, err := suite.app.EvmKeeper.EVMConfig(suite.ctx, suite.ctx.BlockHeader().ProposerAddress, suite.app.EvmKeeper.ChainID())
			suite.Require().NoError(err)

			msg, baseFee, err := newEthMsgTx(
				vmdb.GetNonce(suite.address),
				suite.ctx.BlockHeight(),
				suite.address,
				chainCfg,
				suite.signer,
				signer,
				ethtypes.AccessListTxType,
				nil,
				nil,
				big.NewInt(amountToTransfer),
			)
			suite.Require().NoError(err)

			tx := msg.AsTransaction()

			ethMessage, err := tx.AsMessage(signer, baseFee)
			suite.Require().NoError(err)

			txConfig := suite.app.EvmKeeper.TxConfig(suite.ctx, tx.Hash())
			txContext, err := keeper.CreateSGXVMContext(suite.ctx, suite.app.EvmKeeper, tx)
			suite.Require().NoError(err)

			balanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
			receiverBalanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})

			res, err := suite.app.EvmKeeper.ApplySGXVMMessage(suite.ctx, ethMessage, tc.commit, cfg, txConfig, txContext)
			suite.Require().NoError(err)
			suite.Require().Empty(res.VmError)

			if tc.commit {
				// Check if balance & nonce were updated
				suite.Commit()

				// Check sender's balance
				expectedBalance := balanceBefore.Sub(balanceBefore, big.NewInt(amountToTransfer))
				balanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)

				isSenderBalanceCorrect := expectedBalance.Cmp(balanceAfter)
				suite.Require().True(isSenderBalanceCorrect == 0, "Incorrect sender's balance")

				// Check receiver's balance
				receiverBalanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})
				expectedReceiverBalance := receiverBalanceBefore.Add(receiverBalanceBefore, big.NewInt(amountToTransfer))
				isReceiverBalanceCorrect := expectedReceiverBalance.Cmp(receiverBalanceAfter)
				suite.Require().True(isReceiverBalanceCorrect == 0, "Incorrect receiver's balance")
			} else {
				// Check if balance & nonce still the same
				// Check sender's balance
				balanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
				suite.Require().Equal(balanceBefore, balanceAfter)

				// Check receiver's balance
				receiverBalanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})
				suite.Require().Equal(receiverBalanceBefore, receiverBalanceAfter)
			}
		})
	}
}
