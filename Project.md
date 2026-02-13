# Xenium Blockchain Roadmap

## Phase 0 — Consensus Engine 

**Status:** COMPLETED

Node saat ini sudah mampu menjalankan **single-node deterministic chain simulation**.

**Implemented:**

* **Hybrid consensus**

  * PoH time source (ticks + slots) + deterministic seed for reproducible sim
  * PoS leader selection (epoch stake snapshot)
  * PoV state validation (TxRoot + StateRoot) on-accept
  * Leader snapshot check on-accept (only valid leader can insert block)
* **Deterministic fork-choice:** Weight → Slot → Hash
* **Economic safety:** Reorg guard (MinReorgWeightDeltaP), MaxReorgDepth, N-slot finality (soft finality)
* **Validator discipline:** Slashing, Jailing, Missed slot tracking
* **Epoch system:** Stake snapshot per epoch, Multi-fork candidate ranking
* **Observability:** Reorg severity logging, Fork candidate table, Epoch CSV export, Stake summaries, Fork timeline output

**Current capability:**

* Simulate forks
* Choose canonical chain
* Produce blocks in simulation
* Run with `go run ./cmd/xenium`

> This is the consensus research prototype baseline.

---

## NEXT PHASES (REAL BLOCKCHAIN MODE)

**Transition:** From simulation → real node.

### Phase 1 — Persistent Storage (NEXT 🚨)

**Goal:** Node survives restart and becomes a **long-running blockchain node**.

**Tasks:**

* Add real disk storage:

  * **BlockStore:** SaveBlock, GetBlockByHash, GetBlockByHeight, GetTip, GetRange
  * **SnapshotStore:** SaveEpochSnapshot, LoadLatestSnapshot, LoadSnapshotByEpoch
  * Create: `adapters/storage_file.go`

* **Disk layout:**

```
data/
  blocks.jsonl
  index.json
  snapshots/
    epoch_X.json
```

* Startup behavior:

  * Load latest snapshot
  * Replay blocks after snapshot
  * Rebuild index if missing
  * Restore canonical tip

> After this phase: Node becomes **restartable blockchain node**.

---

### Phase 2 — Real Transaction Pipeline

**Goal:** Blocks contain **real signed transactions**.

**Tasks:**

* Add to Transaction: Nonce, Signature, Hash
* Create **mempool** (thread-safe, fee priority queue, deduplicate tx)
* SelectTxsForBlock()
* Rules:

  * Verify signatures
  * Enforce nonce
  * Check balances
  * Fees go to producer
  * Block invalid if tx invalid

> After this phase: Node becomes **real ledger**.

---

### Phase 3 — State DB + Replay Engine

**Goal:** Deterministic **world state**.

**Tasks:**

* Create StateDB:

  * Account: Balance, Nonce
  * Functions: ApplyTransaction, ApplyBlock, ComputeStateRoot, ExportSnapshot, LoadSnapshot
* Startup replay:

  * Load snapshot
  * Replay blocks
  * Verify state root
  * Restore tip
  * Panic if mismatch

> After this phase: Node becomes **deterministic & verifiable**.

---

### Phase 4 — Real Slot Engine (Wall Clock)

**Goal:** Switch from simulation → **real time**.

* Slot = 2 seconds default
* Deterministic slot from genesis time
* Emit slot events
* Trigger leader election

> After this phase: Node becomes **live ticking blockchain**.

---

### Phase 5 — TCP P2P Network

**Goal:** Nodes **talk to each other**.

**Tasks:**

* Network adapter: `adapters/network_tcp.go`
* Messages: HELLO, STATUS, NEW_TX, NEW_BLOCK, REQUEST_BLOCKS, BLOCK_RESPONSE
* Node must:

  * Connect to peers
  * Gossip blocks & tx
  * Request missing blocks
  * Handle peers concurrently

> After this phase: Node becomes **multi-node network**.

---

### Phase 6 — Chain Sync

**Goal:** New node **joins network automatically**.

* Node must:

  * Handshake peers
  * Compare heights
  * Download blocks
  * Verify consensus rules
  * Reach canonical tip

> After this phase: Node becomes **self-syncing**.

---

### Phase 7 — BFT Finality Voting

**Goal:** Implement **real finality layer**.

* Create voting module: Vote → Validator, Slot, BlockHash, Signature
* Rules:

  * Vote every slot
  * Weight by stake snapshot
  * Finalize at ≥ 2/3 stake
  * Reject forks below checkpoint

> After this phase: Node becomes **Byzantine-fault tolerant**.

---

### Phase 8 — Validator Mode (CLI)

* Add CLI flags: `--validator`, `--validator-key`, `--port`, `--peers`, `--data-dir`
* Validator must:

  * Produce blocks when leader
  * Sign votes
  * Broadcast blocks & votes

> After this phase: Node becomes **real validator client**.

---

### Phase 9 — HTTP RPC

**Public API endpoints:**

* GET /status
* GET /balance/{address}
* GET /block/{height}
* POST /tx/send
* GET /peers

> After this phase: Node becomes **usable blockchain node**.

---

## Final Target

Multiple nodes can:

* Restart safely
* Sync chain automatically
* Gossip transactions
* Produce blocks
* Vote for finality
* Recover after crash
