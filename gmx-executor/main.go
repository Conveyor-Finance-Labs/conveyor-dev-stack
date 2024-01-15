// gmx-executor — daemon that detects LocalGmxMock OrderCreated and auto-calls executeOrder.
// Deterministically settles GMX 2-step async locally (settlement delay via EXECUTOR_DELAY_MS). docs/50 §4.
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const executorABI = `[
  {"type":"event","name":"OrderCreated","inputs":[
     {"name":"orderKey","type":"bytes32","indexed":true},
     {"name":"vault","type":"address","indexed":true},
     {"name":"kind","type":"uint8","indexed":false}]},
  {"type":"function","name":"executeOrder","stateMutability":"nonpayable",
     "inputs":[{"name":"orderKey","type":"bytes32"}],"outputs":[]}
]`

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Read one key from addresses/<profile>.json (manifest written by deployer, docs/20 §6)
func readAddr(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func main() {
	rpc := env("RPC_URL", "http://127.0.0.1:8545")
	gmxHex := env("GMX_ADDRESS", "")
	if gmxHex == "" {
		if f := env("ADDRESSES_FILE", ""); f != "" {
			gmxHex = readAddr(f, "gmxAdapter")
		}
	}
	gmxAddr := common.HexToAddress(gmxHex)
	// anvil default account[0] key (local only)
	pk := strings.TrimPrefix(env("KEEPER_PK", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"), "0x")
	delayMs, _ := strconv.Atoi(env("EXECUTOR_DELAY_MS", "1500"))
	pollMs, _ := strconv.Atoi(env("POLL_MS", "1000"))

	if gmxAddr == (common.Address{}) {
		log.Fatal("GMX_ADDRESS required")
	}

	cl, err := ethclient.Dial(rpc)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	chainID, err := cl.ChainID(context.Background())
	if err != nil {
		log.Fatalf("chainid: %v", err)
	}
	key, err := crypto.HexToECDSA(pk)
	if err != nil {
		log.Fatalf("key: %v", err)
	}
	from := crypto.PubkeyToAddress(*key.Public().(*ecdsa.PublicKey))

	parsed, _ := abi.JSON(strings.NewReader(executorABI))
	orderCreatedTopic := parsed.Events["OrderCreated"].ID
	exec := parsed.Methods["executeOrder"]

	log.Printf("gmx-executor up: rpc=%s gmx=%s from=%s delay=%dms", rpc, gmxAddr.Hex(), from.Hex(), delayMs)

	from_block, _ := cl.BlockNumber(context.Background())
	seen := map[common.Hash]bool{}

	for {
		time.Sleep(time.Duration(pollMs) * time.Millisecond)
		to, err := cl.BlockNumber(context.Background())
		if err != nil || to < from_block {
			continue
		}
		logs, err := cl.FilterLogs(context.Background(), ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from_block),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: []common.Address{gmxAddr},
			Topics:    [][]common.Hash{{orderCreatedTopic}},
		})
		if err != nil {
			log.Printf("filter: %v", err)
			continue
		}
		for _, lg := range logs {
			if len(lg.Topics) < 2 {
				continue
			}
			orderKey := lg.Topics[1]
			if seen[orderKey] {
				continue
			}
			seen[orderKey] = true
			go func(k common.Hash) {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
				if err := send(cl, key, from, chainID, gmxAddr, exec, k); err != nil {
					log.Printf("executeOrder %s: %v", k.Hex(), err)
				} else {
					log.Printf("executed order %s", k.Hex())
				}
			}(orderKey)
		}
		from_block = to + 1
	}
}

func send(cl *ethclient.Client, key *ecdsa.PrivateKey, from common.Address, chainID *big.Int,
	to common.Address, m abi.Method, orderKey common.Hash) error {
	data, err := m.Inputs.Pack(orderKey)
	if err != nil {
		return err
	}
	calldata := append(m.ID, data...)
	nonce, err := cl.PendingNonceAt(context.Background(), from)
	if err != nil {
		return err
	}
	gasPrice, err := cl.SuggestGasPrice(context.Background())
	if err != nil {
		return err
	}
	tx := types.NewTransaction(nonce, to, big.NewInt(0), 500000, gasPrice, calldata)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return err
	}
	return cl.SendTransaction(context.Background(), signed)
}
