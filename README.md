# RYex Local Dev Stack (local-mock)

Fully offline, deterministic full stack. Uses `LocalGmxMock` + `gmx-executor` (async auto-settlement) instead of real GMX. Design: `docs/50_local_dev_stack.md`.

## Quick Start

```bash
# 1) Contracts only (no node required, inner loop)
cd ../contracts && make test          # 54 tests
make demo                              # end-to-end scenario + settlement table logging

# 2) Full stack (anvil + deploy + auto settlement)
cd ../dev-stack && docker compose up
#   anvil:        localhost:8545 (chainId 31337)
#   addresses:    ../contracts/addresses/local-mock.json (consumed by frontend/BFF)
#   executor:     OrderCreated detection → executeOrder after EXECUTOR_DELAY_MS
```

## Components

| Service | Role |
|--------|------|
| `anvil` | Local EVM (offline, block-time 1s) |
| `deployer` | Runs `forge script Deploy` once → deploy contracts + `addresses/local-mock.json` |
| `gmx-executor` | Polls `LocalGmxMock.OrderCreated` → auto-calls `executeOrder` (2-step async replay) |

`EXECUTOR_DELAY_MS`: `0`=instant settlement (fastest loop) / `1500`=settlement delay ("settling" state machine + UI spinner validation).

## Environment Switching (single codebase, docs/40·50)
| Profile | GMX | Purpose |
|----------|-----|------|
| `local-mock` (this stack) | LocalGmxMock + executor | Daily dev · offline |
| `local-fork` | Forked real GMX | Integration testing (`anvil --fork-url $ARBITRUM_RPC`) |
| `gmx-devnet` / `mainnet-stage` | Real GMX | Demo / rehearsal |

## Verification Status
- `forge test`: 54/54 ✅ · `make demo`: end-to-end ✅ · `gmx-executor`: `go build` ✅
- docker compose: manifest ready (run `docker compose up` where Docker daemon is available).
