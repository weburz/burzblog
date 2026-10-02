# Initial local measurements

Measured with `uv run python scripts/measure.py` on 2026-10-02, CPython 3.14.8,
Linux 6.18.40.1 WSL2 x86_64, local temporary filesystem. This is a single warm
development-machine sample, not a hardware-normalized benchmark or VPS promise.
The script uses 100 documents with 100 repeated short paragraphs each.

| Operation | Observed |
| --- | ---: |
| Create and fsync 100 documents | 0.484 seconds |
| Fresh disk read + parse, median of 100 | 0.685 ms |
| Discover 100 paths | 0.157 ms |
| CLI process through `--help`, median of 10 | 58.007 ms |
| Benchmark parent process peak RSS | 35,624 KiB |

Peak RSS includes interpreter, imports and the measurement workload. There is
no idle server, request latency or frontend bundle yet. Build artifact sizes
are printed by the script and change with documentation/source edits; they
exclude installed runtime dependencies. Measure actual deployment memory and
browser bundles when those components exist. These numbers are informational,
not CI thresholds. Run `make check` before measuring a new revision.
