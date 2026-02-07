# MinerHQ

Real-time monitoring dashboard for NerdQAxe ASIC miners. Track hashrate, temperature, power consumption, and profitability across your entire fleet — with Discord alerts and weekly competitions between your miners.

**Stack:** Go + SQLite + Vanilla JS + WebSocket + Docker

---

## Features

- **Real-time Dashboard** — Hashrate, temperature, power, online/offline status for all miners
- **Live Charts** — Hashrate history (1min, 10min, 1h averages), temperature trends, share difficulty scatter plot
- **Multi-coin Support** — BTC, BCH, DGB, XEC, BC2, BTCS with automatic price updates (Binance + CoinGecko)
- **10 Discord Alert Types** — Color-coded embeds with emoji, per-miner cooldown, individually toggleable
- **Weekly Competitions** — Best Share podium, Block Hunters leaderboard, Money Makers rankings
- **Network Auto-discovery** — Scans your local network to find NerdQAxe miners automatically
- **Per-miner Coin Selection** — Each miner can mine a different coin with independent earnings tracking
- **Earnings Tracker** — Historical and current USD value of all mined coins

## Supported Hardware

| Device | ASIC |
|--------|------|
| NerdQAxe++ | BM1370 |
| NerdQAxe+ | BM1368 |
| NerdAxe++ | BM1370 |
| NerdAxe+ | BM1368 |
| NerdAxe | BM1366 |
| NerdOctaxe | BM1368 |

---

## Quick Start

```bash
git clone https://github.com/camarigor/miner-hq.git
cd miner-hq
docker compose up -d
```

Open `http://localhost:8080` in your browser.

### Adding Miners

1. Go to **Settings** and click **Scan Network** — MinerHQ will auto-discover NerdQAxe devices on your local network
2. Or add miners manually by IP address

> **Note:** The Docker container runs in `host` network mode to enable local network scanning.

---

## Configuration

All settings are available in the **Settings** page of the web UI. Configuration is persisted to `/data/config.json` inside the container.

### Discord Webhooks

MinerHQ sends alerts as rich embeds to a Discord channel via webhooks.

#### Creating a Webhook

1. Open your Discord server
2. Go to **Server Settings** > **Integrations** > **Webhooks**
3. Click **New Webhook**
4. Choose the target channel and copy the **Webhook URL**
5. Paste the URL in MinerHQ **Settings** > **Alerts** > **Discord Webhook URL**
6. Click **Test Alert** to verify — you should see a green "Test Alert" embed

#### Required Discord Settings

Without these settings, alert embeds will arrive as **empty messages** with no visible content:

**Channel Permission (server admin):**
- Right-click the channel > **Edit Channel** > **Permissions** > **@everyone**
- Enable **Embed Links** (green checkmark, not neutral gray)

**User Setting (each user):**
- Go to **User Settings** > **Text & Images**
- Enable **Show embeds and preview website links pasted into chat**

> If you see messages from "Miner HQ" with no content, check both settings above.

### Alerts

MinerHQ supports 10 alert types. Each can be individually enabled or disabled in Settings.

| Alert | Emoji | Trigger | Cooldown |
|-------|-------|---------|----------|
| **Miner Offline** | 🔴 | No response for X seconds | 5 min |
| **High Temperature** | 🌡️ | Temperature exceeds threshold | 5 min |
| **Hashrate Drop** | 📉 | Hashrate drops more than X% between polls | 5 min |
| **Share Rejected** | ❌ | Pool rejects a submitted share | 5 min |
| **Pool Disconnected** | 🔌 | Stratum connection lost | 5 min |
| **Low Fan Speed** | 💨 | Fan RPM below minimum | 5 min |
| **Weak WiFi Signal** | 📶 | WiFi RSSI below threshold (dBm) | 5 min |
| **New Best Difficulty** | 🏆 | New session best share difficulty | 5 min |
| **Block Found** | ⛏️ | Miner finds a valid block | None |
| **New Weekly Leader** | 👑 | A different miner takes the weekly lead | None |

**Cooldown** prevents alert spam — each alert type has a 5-minute cooldown per miner. Block Found and New Weekly Leader have no cooldown since they are rare events.

**Testing alerts by type:**
```bash
# Test a specific alert type
curl -X POST http://localhost:8080/api/alerts/test \
  -H 'Content-Type: application/json' \
  -d '{"type": "block_found"}'

# Test all 10 types
for t in miner_offline temp_high hashrate_drop share_rejected \
         pool_disconnected fan_low wifi_weak new_best_diff \
         block_found new_leader; do
  curl -s -X POST http://localhost:8080/api/alerts/test \
    -H 'Content-Type: application/json' \
    -d "{\"type\":\"$t\"}"
  sleep 2
done
```

### Energy

Configure your electricity cost per kWh and currency (USD, EUR, BRL) to calculate daily energy costs in the dashboard.

### Data Retention

| Data | Default Retention |
|------|-------------------|
| Metrics (snapshots) | 30 days |
| Shares | 7 days |
| Blocks | Permanent |

Use the **Purge** button in Settings to manually delete old data. Database size is displayed in Settings.

---

## Competitions

MinerHQ turns solo mining into a game with three weekly competitions between your miners.

### Weekly Best Share

The miner with the highest share difficulty each week wins the crown.

- **Resets** every Sunday at midnight
- **Podium** shows top 3 with rank, percentage of leader, and personal best
- **New record** badge when a miner beats their all-time best
- **New Weekly Leader** alert fires when a different miner takes the #1 spot

### Block Hunters

Ranks miners by blocks found. Titles are earned based on all-time block count:

| Blocks | Title | Icon |
|--------|-------|------|
| 1 | Block Finder | 🔨 |
| 2+ | Block Hunter | ⛏️ |
| 3+ | Block Master | 💎 |
| 4+ | Block Champion | 🏆 |
| 6+ | Block King | 👑 |
| 8+ | Block God | 🌟 |

Tracks consecutive-week streaks (weeks with at least one block found).

### Money Makers

Ranks miners by total USD earned from block rewards.

| Earnings | Title | Icon |
|----------|-------|------|
| > $0 | First Dollar | 💲 |
| $10+ | Coin Collector | 🪙 |
| $100+ | Money Maker | 💵 |
| $500+ | Cash Master | 💰 |
| $1,000+ | Profit King | 👑 |
| $5,000+ | Mining Tycoon | 🏆 |
| $10,000+ | Crypto Mogul | 💎 |

Shows both historical value (price when mined) and current value (today's price).

---

## API Reference

### Miners
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/miners` | List all miners with latest snapshot |
| GET | `/api/miners/{ip}` | Single miner details |
| GET | `/api/miners/{ip}/history` | Historical snapshots |
| POST | `/api/miners` | Add miner by IP |
| DELETE | `/api/miners/{ip}` | Remove miner |
| PUT | `/api/miners/{ip}/coin` | Set coin for miner |

### Stats & History
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/stats` | Fleet aggregate stats |
| GET | `/api/history` | Aggregated hashrate history |

### Shares & Blocks
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/shares` | Recent shares |
| GET | `/api/shares/best` | Best shares (all-time + session) |
| GET | `/api/blocks` | Found blocks |
| GET | `/api/blocks/count` | Total block count |

### Competition
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/competition/weekly` | Weekly best share + block hunters |
| GET | `/api/competition/moneymakers` | Money makers leaderboard |

### Configuration & Tools
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/settings` | Current configuration |
| POST | `/api/settings` | Save configuration |
| POST | `/api/alerts/test` | Send test alert (optional `{"type": "..."}`) |
| POST | `/api/scan` | Scan network for miners |
| GET | `/api/coins` | Supported coins with prices |
| GET | `/api/earnings` | Earnings breakdown per coin |

### Real-time
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/ws` | WebSocket (share, snapshot, block events) |

---

## Development

### Running without Docker

```bash
go build -o minerhq ./cmd/minerhq
./minerhq -config config.json
```

### Project Structure

```
cmd/minerhq/         # Application entrypoint
internal/
  alerts/            # Discord alert engine (10 types, cooldowns, embeds)
  api/               # HTTP handlers, WebSocket hub, event forwarding
  collector/         # Miner polling, share/block parsing, WebSocket client
  config/            # Configuration loading and persistence
  pricing/           # Coin prices (Binance/CoinGecko), block rewards
  scanner/           # Network auto-discovery for NerdQAxe devices
  storage/           # SQLite database, models, queries
web/
  templates/         # HTML (SPA)
  static/css/        # Styles
  static/js/         # Frontend application
```

---

## License

MIT
