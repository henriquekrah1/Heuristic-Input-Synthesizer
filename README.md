# Heuristic-Input-Synthesizer
A high-performance Windows utility written in Go for generating organic, human-like input telemetry. This project explores the intersection of Win32 API interaction and heuristic signal generation to simulate user activity that evades basic pattern recognition.

## 🚀 Technical Highlights

- **Non-Linear Pathing:** Utilizes quadratic Bezier curves to move the cursor, avoiding the perfectly straight lines typical of basic automation.
- **Micro-Drift Physics:** Implements a slow "idle drift" state that mimics the natural tremors and micro-adjustments of a human hand resting on a mouse.
- **Hardware-First Interrupts:** Uses a `SYNTHETIC_MARKER` system to distinguish between generated events and physical hardware input. The synthesizer automatically pauses when a human takes control.
- **Safe Zone Constraint:** Logic-bound to the central 50% of the screen to prevent accidental interaction with taskbars, system trays, or UI borders.
- **Zero External Dependencies:** Built using direct `uintptr` syscalls to `user32.dll` and `kernel32.dll` for a minimal, stealthy footprint.

## ⚡ Quick Start (No Compilation Required)

If you prefer not to build from source, you can download the pre-compiled binary (`synthesizer.exe`) directly from the **[Releases](https://github.com/henriquekrah1/heuristic-input-synthesizer/releases)** section of this repository.

## 🛠 Prerequisites

- **OS:** Windows 10/11
- **Language:** [Go (Golang)](https://go.dev/dl/) 1.20+

## 📦 Installation & Building

1. **Clone the repository:**
   ```bash
   git clone https://github.com/henriquekrah1/Heuristic-Input-Synthesizer
   cd heuristic-input-synthesizer
   ```

2. **Initialize dependencies:**
   ```bash
   go mod init input-synthesizer
   go get golang.org/x/sys/windows
   ```

3. **Build the binary:**
   ```bash
   go build -ldflags="-s -w" -o synthesizer.exe main.go
   ```
   *(The -ldflags strip debug information to reduce file size and increase stealth.)*

## 📖 Usage

Run the executable and enter the desired duration (in minutes) when prompted.

```bash
./synthesizer.exe
```

The application will provide a short delay before starting, allowing you to switch focus to the target environment.

## ⚖️ License

Distributed under the MIT License. See `LICENSE` for more information.
