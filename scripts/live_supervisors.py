#!/usr/bin/env python3
"""The live runit, s6 and OpenRC tests, in their containers (MADR 0212 D13-A; `make live-supervisors`).

Usage: scripts/live_supervisors.py [runit] [s6] [openrc]   (default: all three)

For each kind it builds the image from test/supervisors/Containerfile.<kind>, then cross-compiles for the
image's Linux architecture, once: mcremote, scripts/smoke-protocol, test/supervisors/probeagent, and the
service package's test binary (`go test -c`). In a fresh container it copies them in and runs TestLive<Kind>
as the non-root `user` user, with MCREMOTE_REQUIRE_<KIND>=1. The container is removed afterwards. The test
updates to a published release from GitHub; GH_TOKEN or GITHUB_TOKEN, when set, is passed through to raise
the rate limit, by name, never on a command line.

Environment:
  CONTAINER_ENGINE        podman (the default) or docker
  MCREMOTE_LIVE_RELEASE   the release to update to (the test's own default otherwise)

Exit status: 1 if any kind failed or could not run.
"""

from __future__ import annotations

import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
IMAGES = ROOT / "test" / "supervisors"
KINDS = {"runit": ("RUNIT", "TestLiveRunit"), "s6": ("S6", "TestLiveS6"), "openrc": ("OPENRC", "TestLiveOpenRC")}
ENGINE = os.environ.get("CONTAINER_ENGINE", "podman")
REMOTE = "/home/user/live"
BUILDS = {
    "mcremote": ["go", "build", "-o", "{out}", "./cmd/mcremote"],
    "smoke-protocol": ["go", "build", "-o", "{out}", "./scripts/smoke-protocol"],
    "probeagent": ["go", "build", "-o", "{out}", "./test/supervisors/probeagent"],
    "live.test": ["go", "test", "-c", "-o", "{out}", "./internal/cli/service/"],
}


def say(line: str) -> None:
    print(line, flush=True)


def engine(*args: str, check: bool = True, timeout: int = 1800) -> subprocess.CompletedProcess:
    r = subprocess.run([ENGINE, *args], capture_output=True, text=True, timeout=timeout)
    if check and r.returncode != 0:
        raise RuntimeError(f"{ENGINE} {' '.join(args[:3])}: exit {r.returncode}\n{r.stdout}{r.stderr}")
    return r


def goarch(image: str) -> str:
    machine = engine("run", "--rm", image, "uname", "-m").stdout.strip()
    return {"x86_64": "amd64", "aarch64": "arm64"}[machine]


def build(arch: str, out: pathlib.Path) -> None:
    env = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0")
    for name, cmd in BUILDS.items():
        r = subprocess.run([c.format(out=out / name) for c in cmd], cwd=ROOT, env=env, capture_output=True, text=True)
        if r.returncode != 0:
            raise RuntimeError(f"build {name} for linux/{arch}: exit {r.returncode}\n{r.stdout}{r.stderr}")
    say(f"built mcremote, smoke-protocol, probeagent and live.test for linux/{arch}")


def run_kind(kind: str, image: str, out: pathlib.Path) -> bool:
    upper, test = KINDS[kind]
    name = f"mcremote-live-{kind}-{os.getpid()}"
    engine("rm", "-f", name, check=False)
    engine("run", "-d", "--name", name, image)
    try:
        engine("exec", "-u", "root", name, "sh", "-c",
               f"mkdir -p /run/user/1000 {REMOTE} && chown user /run/user/1000 && chmod 700 /run/user/1000")
        for f in BUILDS:
            engine("cp", str(out / f), f"{name}:{REMOTE}/{f}")
        engine("exec", "-u", "root", name, "chown", "-R", "user", REMOTE)
        env = ["XDG_RUNTIME_DIR=/run/user/1000", "HOME=/home/user", f"MCREMOTE_REQUIRE_{upper}=1",
               f"MCREMOTE_LIVE_BINARY={REMOTE}/mcremote", f"MCREMOTE_LIVE_SMOKE={REMOTE}/smoke-protocol",
               f"MCREMOTE_LIVE_AGENT={REMOTE}/probeagent"]
        if os.environ.get("MCREMOTE_LIVE_RELEASE"):
            env.append(f"MCREMOTE_LIVE_RELEASE={os.environ['MCREMOTE_LIVE_RELEASE']}")
        passed = dict(os.environ)
        token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
        flags = [x for e in env for x in ("-e", e)]
        if token:
            passed["GH_TOKEN"] = token
            flags += ["-e", "GH_TOKEN"]  # by name: the value comes from this process's environment
        cmd = [ENGINE, "exec", "-u", "user", *flags, "-w", "/home/user", name, f"{REMOTE}/live.test",
               "-test.run", f"^{test}$", "-test.count=1", "-test.v", "-test.timeout=20m"]
        say(f"===== {kind}: {test} in {image}")
        code = subprocess.run(cmd, env=passed, timeout=1500).returncode
        say(f"===== {kind}: exit {code}")
        return code == 0
    finally:
        engine("rm", "-f", name, check=False)


def main() -> int:
    kinds = sys.argv[1:] or list(KINDS)
    unknown = [k for k in kinds if k not in KINDS]
    if unknown:
        say(f"unknown kinds {unknown}; known: {list(KINDS)}")
        return 2
    out = pathlib.Path(tempfile.mkdtemp(prefix="mcremote-live-"))
    built: str | None = None
    failed: list[str] = []
    try:
        for kind in kinds:
            image = f"mcremote-live-{kind}"
            engine("build", "-t", image, "-f", str(IMAGES / f"Containerfile.{kind}"), str(IMAGES), timeout=3600)
            arch = goarch(image)
            if built != arch:
                build(arch, out)
                built = arch
            try:
                if not run_kind(kind, image, out):
                    failed.append(kind)
            except (RuntimeError, subprocess.TimeoutExpired) as e:
                say(f"===== {kind}: could not run: {e}")
                failed.append(kind)
    finally:
        shutil.rmtree(out, ignore_errors=True)
    say(f"live supervisors: {', '.join(kinds)}; failed: {', '.join(failed) or 'none'}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
