"""Shared formatted console reporter for DockVault E2E test suites."""

import os
import sys
import time
from contextlib import contextmanager


class _Style:
    def __init__(self):
        enabled = sys.stdout.isatty() and not os.environ.get("NO_COLOR")
        self.bold = "\033[1m" if enabled else ""
        self.dim = "\033[2m" if enabled else ""
        self.green = "\033[32m" if enabled else ""
        self.red = "\033[31m" if enabled else ""
        self.cyan = "\033[36m" if enabled else ""
        self.reset = "\033[0m" if enabled else ""


class Suite:
    """Clean tabular scenario runner emitting aligned PASS/FAIL timings and summary."""

    def __init__(self, title, total):
        self.title = title
        self.total = total
        self.passed = 0
        self.started_at = time.monotonic()
        self._cur_t0 = None
        self.s = _Style()
        bar = "─" * max(8, 66 - len(title))
        print(f"\n{self.s.bold}{self.s.cyan}┌─ {title} {bar}{self.s.reset}", flush=True)

    def begin(self, num, label):
        self._cur_t0 = time.monotonic()
        tag = f"[{num:02d}/{self.total:02d}]"
        padded = label.ljust(56, ".")
        print(f"  {self.s.dim}{tag}{self.s.reset} {padded} ", end="", flush=True)

    def ok(self, detail=None):
        dt = time.monotonic() - (self._cur_t0 or self.started_at)
        self._cur_t0 = None
        self.passed += 1
        print(f"{self.s.bold}{self.s.green}PASS{self.s.reset} {self.s.dim}({dt:.2f}s){self.s.reset}", flush=True)
        if detail:
            print(f"          {self.s.dim}└─ {detail}{self.s.reset}", flush=True)

    def fail(self):
        if self._cur_t0 is not None:
            dt = time.monotonic() - self._cur_t0
            self._cur_t0 = None
            print(f"{self.s.bold}{self.s.red}FAIL{self.s.reset} {self.s.dim}({dt:.2f}s){self.s.reset}", flush=True)

    @contextmanager
    def step(self, num, label):
        self.begin(num, label)
        notes = []

        def note(text):
            notes.append(text)

        try:
            yield note
        except Exception:
            self.fail()
            for item in notes:
                print(f"          {self.s.dim}└─ {item}{self.s.reset}", flush=True)
            raise

        self.ok(notes[0] if len(notes) == 1 else None)
        if len(notes) > 1:
            for item in notes:
                print(f"          {self.s.dim}└─ {item}{self.s.reset}", flush=True)

    def finish(self):
        total_dt = time.monotonic() - self.started_at
        print(
            f"{self.s.bold}{self.s.green}└─ {self.title}: "
            f"{self.passed}/{self.total} passed in {total_dt:.2f}s{self.s.reset}\n",
            flush=True,
        )
