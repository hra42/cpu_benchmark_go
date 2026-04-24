# CPU Benchmark — Go / Linux — Projektplan

---

## Ziel

Ein reproduzierbares, portables Benchmark-Tool für Linux/SoC-Systeme das sowohl Single-Thread als auch Multi-Thread Performance misst, normierte Scores ausgibt und Ergebnisse als JSON exportiert um Maschinen direkt vergleichen zu können.

---

## Projektstruktur

```
cpu-benchmark/
├── main.go                  # CLI Entry Point, Flag-Parsing, Orchestrierung
├── runner/
│   └── runner.go            # Benchmark-Loop, Warmup, Zeitmessung, Verifikation
├── benchmarks/
│   ├── primes.go            # Primzahlsieb (Eratosthenes)
│   ├── sorting.go           # Quicksort auf zufälligem Array
│   ├── matrix.go            # Matrixmultiplikation (Integer + Float64)
│   ├── hashing.go           # SHA-256 Throughput
│   └── json.go              # JSON encode/decode
├── scoring/
│   └── scoring.go           # Normierung, Gewichtung, Gesamt-Score, Labels
├── results/
│   └── results.go           # Datenstrukturen, Aggregation, Statistik (Median/P95/P99)
├── output/
│   ├── table.go             # Tabellenausgabe Terminal
│   └── export.go            # JSON Export
└── README.md
```

---

## Runner

Herzstück des Tools. Nimmt jeden Benchmark als Interface entgegen und ist vollständig unabhängig von den einzelnen Algorithmen.

**Interface:**
```go
type Benchmark interface {
    Name()    string
    Tags()    []string
    Setup(n int)
    Run() any       // Gibt Ergebnis zurück (verhindert Compiler-Optimierung)
    Verify(result any) bool
}
```

**Ablauf pro Benchmark:**
1. `Setup()` — Eingabedaten vorbereiten
2. Warmup-Phase (default: 2s) — CPU-Frequenz stabilisieren, Caches aufwärmen
3. `runtime.GC()` — definierten GC-Zustand herstellen
4. Mess-Phase über feste Zeitspanne (default: 10s) — Iterationen zählen, Zeiten sammeln
5. `Verify()` — Ergebnis prüfen damit der Compiler nichts wegoptimiert
6. Statistik berechnen: Ops/Sek, Median, P95, P99

**Single-Thread:** `GOMAXPROCS=1`, eine Goroutine  
**Multi-Thread:** `GOMAXPROCS=runtime.NumCPU()`, Arbeit gleichmäßig auf alle Kerne verteilt

---

## Benchmarks

### Primzahlsieb (`primes.go`)
- Algorithmus: Sieb des Eratosthenes
- Eingabegrößen: N = 1M / 10M / 100M (konfigurierbar)
- Misst: Integer-Throughput, sequentieller Speicherzugriff, Cache-Auslastung
- Verifikation: Anzahl gefundener Primzahlen gegen bekannte Werte

### Quicksort (`sorting.go`)
- Zufälliges `[]int64` Array, jede Iteration neu generiert und sortiert
- Eingabegrößen: 10k / 100k / 1M Elemente
- Misst: Branch Prediction, Cache-Misses bei großen Arrays, Speicherbandbreite
- Verifikation: Array aufsteigend sortiert

### Matrixmultiplikation (`matrix.go`)
- Größen: 64×64 / 256×256 / 512×512
- Integer und Float64 als getrennte Sub-Scores
- Misst: ALU-Throughput, L1/L2/L3 Cache-Verhalten je nach Größe
- Verifikation: Bekanntes Referenzergebnis für kleine Matrizen

### SHA-256 Throughput (`hashing.go`)
- Fixer Puffer (1MB / 64MB) wird wiederholt gehasht
- Misst: Integer-ALU-Intensität, Speicherbandbreite
- Ausgabe: MB/s
- Verifikation: Hash-Ergebnis gegen Referenz

### JSON encode/decode (`json.go`)
- Repräsentative verschachtelte Struct-Typen (mixed types, Arrays, Strings)
- Misst: Allocator-Druck, GC-Verhalten, Reflection-Performance
- GC-Pausen separat ausweisen (via `runtime.ReadMemStats`)
- Ausgabe: Ops/Sek, GC-Pausen Median/P99

---

## Scoring

### Einzelscores

Jeder Benchmark liefert normierte Punkte gegen eine fest definierte Referenz-CPU:

```
Score = (Gemessener Wert / Referenzwert) × 1000
```

Referenz = **1000 Punkte** (Apple M3 Pro, 6P+6E / 12 Threads, macOS, --size medium, Werte hart in `scoring/scoring.go` hinterlegt)

| Score | Zusammensetzung | Gewicht ST/MT |
|---|---|---|
| Integer Score | Primzahlsieb + Quicksort | 30% |
| Memory Score | Matrix (alle Größen) + SHA-256 | 30% |
| Throughput Score | SHA-256 MB/s + JSON Ops/s | 25% |
| Latency Score | P99 aller Benchmarks kombiniert | 15% |

### Gesamt-Score

```
Gesamt = Integer(30%) + Memory(30%) + Throughput(25%) + Latency(15%)
```

Gewichtung ist über Config/Flags anpassbar (z.B. Profil "database", "batch", "default").

Single-Thread und Multi-Thread werden immer getrennt ausgewiesen — kein Mischen.

### Einordnung

| Score | Label |
|---|---|
| < 500 | Embedded / Low-Power |
| 500–900 | Entry Level |
| 900–1100 | Referenz |
| 1100–2000 | Performance |
| 2000–4000 | High Performance |
| > 4000 | Workstation / Server |

---

## Konfiguration

Via CLI-Flags:

```
--duration     Messdauer pro Benchmark        (default: 10s)
--warmup       Warmup-Dauer                   (default: 2s)
--benchmarks   Kommaseparierte Auswahl        (default: all)
--threads      1=Single, N=N-Threads, 0=alle  (default: 0)
--size         small | medium | large          (default: medium)
--output       table | json | both             (default: table)
--profile      default | database | batch      (default: default)
```

---

## Output

### Terminal

```
cpu-benchmark v0.1 — darwin/arm64 — Go 1.26
CPU: Apple M3 Pro — 12 Threads (6P + 6E)
────────────────────────────────────────────────────────────────────
Benchmark          ST Score    MT Score    Median      P99
────────────────────────────────────────────────────────────────────
Integer            1840        5200        68ms        89ms
Memory             1200        1350        1.1ms       1.3ms
Throughput         2100        6800        41ms        44ms
Latency            1650        1700        —           —
────────────────────────────────────────────────────────────────────
Gesamt (ST)        1680        [Performance]
Gesamt (MT)        3900        [High Performance]
────────────────────────────────────────────────────────────────────
Referenz: Apple M3 Pro — Score 1000
```

### JSON Export

```json
{
  "meta": {
    "tool_version": "0.1",
    "timestamp": "2026-04-24T10:00:00Z",
    "go_version": "1.22",
    "cpu_model": "AMD Ryzen 7 5800X",
    "cpu_cores": 8,
    "cpu_threads": 16,
    "os": "linux",
    "arch": "amd64"
  },
  "config": {
    "duration_s": 10,
    "warmup_s": 2,
    "size": "medium",
    "profile": "default",
    "weights": {
      "integer": 0.30,
      "memory": 0.30,
      "throughput": 0.25,
      "latency": 0.15
    }
  },
  "scores": {
    "single_thread": {
      "integer": 1840,
      "memory": 1200,
      "throughput": 2100,
      "latency": 1650,
      "total": 1680,
      "label": "Performance"
    },
    "multi_thread": {
      "integer": 5200,
      "memory": 1350,
      "throughput": 6800,
      "latency": 1700,
      "total": 3900,
      "label": "High Performance"
    }
  },
  "raw": {
    "primes_10m": {
      "ops_sec": 142,
      "median_ns": 68200000,
      "p95_ns": 71400000,
      "p99_ns": 89100000,
      "iterations": 1420
    }
  }
}
```

---

## Reihenfolge der Implementierung

1. **Projektstruktur + Interfaces** — `runner.go`, `results.go`, Benchmark-Interface definieren
2. **Erster Benchmark** — `primes.go` als Referenzimplementierung
3. **Runner vollständig** — Warmup, Mess-Loop, Statistik, Verifikation
4. **Restliche Benchmarks** — `sorting.go`, `matrix.go`, `hashing.go`, `json.go`
5. **Scoring** — Normierung, Gewichtung, Labels
6. **Output** — Terminal-Tabelle, JSON Export
7. **CLI + Konfiguration** — Flags, Profile
8. **Referenzwerte kalibrieren** — auf Referenz-CPU (Apple M3 Pro) laufen lassen, Werte in `scoring/scoring.go` eintragen ✅

