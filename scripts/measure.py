"""Measure local foundation operations; no server or idle-memory claim."""

import json
import platform
import resource
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

from burzblog import ContentStore, Document


def main() -> None:
    """Measure 100 files, 100 fresh reads, and 10 separate CLI startups."""
    with tempfile.TemporaryDirectory(prefix="burzblog-measure-") as directory:
        store = ContentStore(directory)
        document = Document.new("Benchmark", "benchmark", "Paragraph.\n" * 100)
        started = time.perf_counter()
        for index in range(100):
            store.create(f"post-{index}.md", document)
        creation = time.perf_counter() - started
        samples = []
        for index in range(100):
            started = time.perf_counter()
            store.read(f"post-{index}.md")
            samples.append((time.perf_counter() - started) * 1000)
        started = time.perf_counter()
        if len(store.discover()) != 100:
            raise RuntimeError("Benchmark fixture has an unexpected file count.")
        discovery = (time.perf_counter() - started) * 1000
        startups = []
        executable = Path(sys.executable).with_name("burzblog")
        for _ in range(10):
            started = time.perf_counter()
            subprocess.run(  # noqa: S603 - fixed local executable and arguments
                [str(executable), "--help"], check=True, stdout=subprocess.DEVNULL
            )
            startups.append((time.perf_counter() - started) * 1000)
        print(
            json.dumps(
                {
                    "platform": platform.platform(),
                    "python": platform.python_version(),
                    "files": 100,
                    "create_100_seconds": round(creation, 3),
                    "read_median_ms": round(statistics.median(samples), 3),
                    "discover_100_ms": round(discovery, 3),
                    "cli_startup_median_ms": round(statistics.median(startups), 3),
                    "benchmark_process_peak_rss_platform_units": resource.getrusage(
                        resource.RUSAGE_SELF
                    ).ru_maxrss,
                    "artifacts_bytes": {
                        path.name: path.stat().st_size
                        for path in sorted(Path("dist").glob("burzblog-0.1.0*"))
                    },
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
