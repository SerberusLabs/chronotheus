# Chronotheus

> A slightly scatter-brained Prometheus proxy that sneaks in historical slices alongside "now" so Grafana can plot everything without extra plugins. A coffee and Amphetamine fulled Hyperfocus conversion from PHP to Go.

Chronotheus will:

1. Hit your Prometheus **five** times—now, 7d, 14d, 21d & 28d ago
2. Shift all those timestamps **forward** so they line up with today
3. Tag each series with `chrono_timeframe="current|7days|14days|21days|28days"`
4. Build three extra series per metric:

- **lastMonthAverage**: minute-by-minute average of those four past windows
- **compareAgainstLast28**: raw difference (current − average)
- **percentCompareAgainstLast28**: percent difference ((current − avg)/avg × 100)

5. Carry along any `_command="…"` flag you sneak into your PromQL
6. Strip out those synthetic labels before talking to the real Prometheus (no stray commas left behind, promise)

---

## 🛠️ Prerequisites

- **Go 1.18+** installed and on your `PATH`
- A running **Prometheus** instance you can reach
- A caffeine or snack of your choice—you'll need it to read this README

---

## 🏗️ Building

1. Clone this repo and cd in:
   `bash
  git clone https://github.com/andydixon/chronotheus.git
  cd chronotheus
  `
2. Initialize modules & fetch dependencies:
   `bash
  go mod tidy
  `
3. (Optional but highly recommended) Run the tests to make sure all helpers behave:
   `bash
  go test ./proxy
  `
4. Build the binary:
   `bash
  go build -o chronotheus main.go
  `
5. You'll now have a `./chronotheus` executable ready to rock.

---

## ▶️ Running

Simply:

`bash
./chronotheus
`

You should see:

`🚀 Chronotheus proxy listening on :8080`

(It defaults to port 8080. You can slap it behind supervisor, a systemd unit, or 🔥 PM2 if you're feeling fancy.)

---

## ⚙️ Registering in Grafana

1. In Grafana, go to **Configuration → Data Sources → Add data source**.
2. Pick **Prometheus** (yes, even though it's Chronotheus under the hood).
3. Set the **URL** to:

```
http://<HOST>:8080/<UPSTREAM_PROM_HOST>_<UPSTREAM_PROM_PORT>
`
  For example:
  `
http://localhost:8080/prometheus_9090
``
4. Leave **Access** on "Server (default)".
5. Click **Save & test**—it should say "Data source is working".

Now when you query in Grafana, prepend nothing! Just use your normal PromQL:

- `my_metric` → returns all timeframes + averages + diffs
- `my_metric{chrono_timeframe="14days"}` → just that slice
- `my_metric{chrono_timeframe="percentCompareAgainstLast28"}` → percent diffs

---

## 📂 Project Layout

``
chronotheus/
├── go.mod ← Go module file
├── main.go ← server bootstrap
└── proxy/
├── proxy.go ← HTTP routing & ServeHTTP
├── handlers.go ← /query, /query_range, /labels, /label/... logic
└── utils.go ← helpers: parsing, stripping, averaging, dedupe, etc.
```

---

## 🔍 Endpoints

| Path                          | Methods   | Description                                                  |
| ----------------------------- | --------- | ------------------------------------------------------------ |
| `/api/v1/query`               | GET, POST | Instant vector with all historical slices & synthetic series |
| `/api/v1/query_range`         | GET, POST | Range matrix with all historical slices & synthetic series   |
| `/api/v1/labels`              | GET, POST | List labels **plus**`chrono_timeframe`                       |
| `/api/v1/label/{name}/values` | GET, POST | List values (special case for`chrono_timeframe`)             |
| `/*`                          | any       | Reverse-proxies any other path unchanged                     |

---

## 🐞 Troubleshooting

- **404 errors?** Make sure you included the`/host_port/`prefix in your Grafana URL.
- **No data for a specific slice?** Double-check your`chrono_timeframe="…"`spelling.
- **Upstream failures?** Look at Chronotheus's logs (stderr) for`[Chrono][Upstream ERROR]`.
- **PromQL maths not working?** PromQL queries using the synthetic metrics will not work since they are stripped before going upstream, so it would just be querying the raw data.

---

## 📜 License

GPLv3 © Andy Dixon
(Feel free to fork, hack, snack, repeat.)
