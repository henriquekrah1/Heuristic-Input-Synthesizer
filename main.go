package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── Win32 bindings ────────────────────────────────────────────────────────────

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

const (
	INPUT_MOUSE    = 0
	INPUT_KEYBOARD = 1

	MOUSEEVENTF_MOVE     = 0x0001
	MOUSEEVENTF_LEFTDOWN = 0x0002
	MOUSEEVENTF_LEFTUP   = 0x0004
	MOUSEEVENTF_WHEEL    = 0x0800
	MOUSEEVENTF_ABSOLUTE = 0x8000
	MOUSE_NORM           = 65535

	WHEEL_DELTA     = 120
	KEYEVENTF_KEYUP = 0x0002
	VK_SHIFT        = uint16(0x10)

	// Unique marker stamped on every synthetic event's dwExtraInfo.
	// We record the tick just before each send; if GetLastInputInfo
	// returns a tick newer than ours, a real human event occurred.
	SYNTHETIC_MARKER = uintptr(0xABCD1234)
)

type POINT struct{ X, Y int32 }

type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

type MOUSEINPUT struct {
	Dx, Dy      int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KEYBDINPUT struct {
	WVk, WScan  uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type INPUT struct {
	Type uint32
	_    [4]byte
	Mi   MOUSEINPUT
}

type KBINPUT struct {
	Type uint32
	_    [4]byte
	Ki   KEYBDINPUT
}

// ── Screen / cursor helpers ───────────────────────────────────────────────────

func screenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(0)
	h, _, _ := procGetSystemMetrics.Call(1)
	return int(w), int(h)
}

func cursorPos() (int, int) {
	var p POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y)
}

// ── User-activity detection ───────────────────────────────────────────────────

var lastSyntheticTick atomic.Uint32

func tickCount() uint32 {
	t, _, _ := procGetTickCount.Call()
	return uint32(t)
}

func lastInputTick() uint32 {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))
	procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	return lii.DwTime
}

// Call immediately before every SendInput so we can distinguish our events.
func markSynthetic() {
	lastSyntheticTick.Store(tickCount())
}

// True if a real human input event arrived after our last synthetic one.
func humanInputDetected() bool {
	last := lastInputTick()
	synthetic := lastSyntheticTick.Load()
	return last > synthetic+50 // 50ms grace for timing jitter
}

var userActive atomic.Bool

// watchInput runs in its own goroutine.
// Flips userActive true on human input, then waits for 3 consecutive
// seconds of no new input before flipping back.
func watchInput(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		if humanInputDetected() && !userActive.Load() {
			fmt.Println("⏸  User activity detected — pausing...")
			userActive.Store(true)

			// Wait for 3s of clean idle
			lastTick := lastInputTick()
			idleStart := time.Now()
			for {
				time.Sleep(250 * time.Millisecond)
				current := lastInputTick()
				if current != lastTick {
					lastTick = current
					idleStart = time.Now() // reset on new input
				}
				if time.Since(idleStart) >= 3*time.Second {
					break
				}
			}

			// Refresh synthetic marker so we don't immediately re-trigger
			markSynthetic()
			fmt.Println("▶  Resuming...")
			userActive.Store(false)
		}

		time.Sleep(150 * time.Millisecond)
	}
}

// ── Input primitives ──────────────────────────────────────────────────────────

func sendMouse(x, y int) {
	sw, sh := screenSize()
	nx := int(math.Round(float64(x) * MOUSE_NORM / float64(sw)))
	ny := int(math.Round(float64(y) * MOUSE_NORM / float64(sh)))
	markSynthetic()
	inp := INPUT{Type: INPUT_MOUSE, Mi: MOUSEINPUT{
		Dx:          int32(nx),
		Dy:          int32(ny),
		DwFlags:     MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE,
		DwExtraInfo: SYNTHETIC_MARKER,
	}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
}

func bezierMove(x0, y0, x1, y1 int) {
	steps := 25 + rand.Intn(15)
	cpx := (x0+x1)/2 + rand.Intn(80) - 40
	cpy := (y0+y1)/2 + rand.Intn(80) - 40

	for i := 0; i <= steps; i++ {
		if userActive.Load() {
			return
		}
		t := float64(i) / float64(steps)
		x := int(math.Round((1-t)*(1-t)*float64(x0) + 2*(1-t)*t*float64(cpx) + t*t*float64(x1)))
		y := int(math.Round((1-t)*(1-t)*float64(y0) + 2*(1-t)*t*float64(cpy) + t*t*float64(y1)))
		sendMouse(x, y)

		delay := 5 + rand.Intn(8)
		if i < 4 || i > steps-4 {
			delay += 6
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

// safeZone returns the central 50% rectangle of the screen.
// Keeps all movement and clicks away from edges, taskbars,
// browser tabs, and UI buttons near screen borders.
func safeZone(sw, sh int) (x0, y0, x1, y1 int) {
	x0 = sw / 4
	y0 = sh / 4
	x1 = sw * 3 / 4
	y1 = sh * 3 / 4
	return
}

func wander(sw, sh int) {
	zx0, zy0, zx1, zy1 := safeZone(sw, sh)
	cx, cy := cursorPos()

	// Nudge cursor into safe zone if it starts outside (e.g. after user moves it)
	cx = clamp(cx, zx0, zx1)
	cy = clamp(cy, zy0, zy1)

	drift := 200
	tx := clamp(cx+rand.Intn(drift*2)-drift, zx0, zx1)
	ty := clamp(cy+rand.Intn(drift*2)-drift, zy0, zy1)
	bezierMove(cx, cy, tx, ty)
}

func scroll(dir int) {
	ticks := 1 + rand.Intn(3)
	for i := 0; i < ticks; i++ {
		if userActive.Load() {
			return
		}
		markSynthetic()
		inp := INPUT{Type: INPUT_MOUSE, Mi: MOUSEINPUT{
			MouseData:   uint32(int32(dir) * WHEEL_DELTA),
			DwFlags:     MOUSEEVENTF_WHEEL,
			DwExtraInfo: SYNTHETIC_MARKER,
		}}
		procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
		time.Sleep(time.Duration(70+rand.Intn(100)) * time.Millisecond)
	}
}

func scrollWhileMoving(sw, sh int) {
	dir := 1
	if rand.Intn(2) == 0 {
		dir = -1
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if !userActive.Load() {
					scroll(dir)
					if rand.Intn(7) == 0 {
						dir = -dir
					}
				}
				jitter(150, 450)
			}
		}
	}()
	wander(sw, sh)
	close(stop)
}

func pressShift() {
	markSynthetic()
	inp := KBINPUT{Type: INPUT_KEYBOARD, Ki: KEYBDINPUT{
		WVk:         VK_SHIFT,
		DwExtraInfo: SYNTHETIC_MARKER,
	}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
	time.Sleep(time.Duration(40+rand.Intn(60)) * time.Millisecond)
	markSynthetic()
	inp.Ki.DwFlags = KEYEVENTF_KEYUP
	procSendInput.Call(1, uintptr(unsafe.Pointer(&inp)), unsafe.Sizeof(inp))
}

func leftClick() {
	markSynthetic()
	down := INPUT{Type: INPUT_MOUSE, Mi: MOUSEINPUT{
		DwFlags:     MOUSEEVENTF_LEFTDOWN,
		DwExtraInfo: SYNTHETIC_MARKER,
	}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&down)), unsafe.Sizeof(down))
	time.Sleep(time.Duration(55+rand.Intn(70)) * time.Millisecond)
	markSynthetic()
	up := INPUT{Type: INPUT_MOUSE, Mi: MOUSEINPUT{
		DwFlags:     MOUSEEVENTF_LEFTUP,
		DwExtraInfo: SYNTHETIC_MARKER,
	}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&up)), unsafe.Sizeof(up))
}

func maybeClick() {
	leftClick()
	if rand.Intn(4) == 0 {
		time.Sleep(time.Duration(70+rand.Intn(50)) * time.Millisecond)
		leftClick()
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func jitter(minMs, maxMs int) {
	time.Sleep(time.Duration(minMs+rand.Intn(maxMs-minMs)) * time.Millisecond)
}

// idleDrift simulates the cursor slowly wandering while the "user is thinking" —
// tiny incremental steps in a loosely consistent direction, occasionally
// changing direction, for the given duration. Replaces dead-still pauses.
func idleDrift(sw, sh int, durationMs int) {
	zx0, zy0, zx1, zy1 := safeZone(sw, sh)
	cx, cy := cursorPos()
	cx = clamp(cx, zx0, zx1)
	cy = clamp(cy, zy0, zy1)

	// Pick a slow drift direction
	dx := float64(rand.Intn(3) - 1) // -1, 0, or 1
	dy := float64(rand.Intn(3) - 1)
	if dx == 0 && dy == 0 {
		dx = 1
	}

	stepSize := 1.0 + rand.Float64()*1.5 // very slow: 1-2.5px per step
	elapsed := 0

	for elapsed < durationMs {
		if userActive.Load() {
			return
		}

		cx = clamp(int(float64(cx)+dx*stepSize), zx0, zx1)
		cy = clamp(int(float64(cy)+dy*stepSize), zy0, zy1)
		sendMouse(cx, cy)

		// Occasionally nudge direction slightly — like a hand resting and shifting
		if rand.Intn(12) == 0 {
			dx += float64(rand.Intn(3)-1) * 0.5
			dy += float64(rand.Intn(3)-1) * 0.5
			// Keep magnitude reasonable
			dx = clampF(dx, -2, 2)
			dy = clampF(dy, -2, 2)
		}

		// Bounce off safe zone edges
		if cx <= zx0 || cx >= zx1 {
			dx = -dx
		}
		if cy <= zy0 || cy >= zy1 {
			dy = -dy
		}

		delay := 40 + rand.Intn(60) // 40-100ms per micro-step = very slow
		time.Sleep(time.Duration(delay) * time.Millisecond)
		elapsed += delay
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Session loop ──────────────────────────────────────────────────────────────

func runSession(minutes int) {
	sw, sh := screenSize()
	end := time.Now().Add(time.Duration(minutes) * time.Minute)

	fmt.Printf("▶ Running for %d minute(s). Ctrl+C to stop early.\n\n", minutes)

	// Prime the synthetic marker before watcher starts to avoid false trigger
	markSynthetic()
	time.Sleep(200 * time.Millisecond)

	stopWatch := make(chan struct{})
	go watchInput(stopWatch)
	defer close(stopWatch)

	for time.Now().Before(end) {
		if userActive.Load() {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		switch rand.Intn(10) {
		case 0, 1, 2, 3, 4:
			wander(sw, sh)
			if !userActive.Load() && rand.Intn(6) == 0 {
				idleDrift(sw, sh, 150+rand.Intn(200))
				maybeClick()
			}
			// Drift slowly between actions instead of freezing
			idleDrift(sw, sh, 600+rand.Intn(1900))

		case 5, 6:
			scrollWhileMoving(sw, sh)
			idleDrift(sw, sh, 600+rand.Intn(1400))

		case 7, 8:
			pressShift()
			idleDrift(sw, sh, 300+rand.Intn(600))
			if !userActive.Load() {
				if rand.Intn(2) == 0 {
					scrollWhileMoving(sw, sh)
				} else {
					wander(sw, sh)
				}
			}
			idleDrift(sw, sh, 600+rand.Intn(1400))

		default:
			// Longer thinking pause — slow drift only
			idleDrift(sw, sh, 2000+rand.Intn(3000))
			if !userActive.Load() {
				wander(sw, sh)
			}
		}

		// Rare extended reading/thinking pause
		if rand.Intn(18) == 0 {
			idleDrift(sw, sh, 3000+rand.Intn(4000))
		}
	}

	fmt.Println("\n✅ Session complete.")
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	rand.Seed(time.Now().UnixNano())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n⛔ Stopped.")
		os.Exit(0)
	}()

	fmt.Println("=== Activity Simulator ===")
	fmt.Print("Minutes to run: ")

	var minutes int
	fmt.Scan(&minutes)

	if minutes < 1 || minutes > 120 {
		fmt.Println("Please enter a value between 1 and 120.")
		os.Exit(1)
	}

	delay := 2 + rand.Intn(4)
	fmt.Printf("\nStarting in %d seconds — switch to your VM!\n", delay)
	time.Sleep(time.Duration(delay) * time.Second)

	runSession(minutes)
}