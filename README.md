# Heuristic-Input-Synthesizer
A low-level Windows utility for simulating organic user input telemetry. It utilizes Win32 API bindings in Go to generate non-linear movement via quadratic Bezier curves and heuristic idle drift. Features a real-time interrupt system that detects physical hardware input to ensure synthetic events never conflict with manual user activity.
