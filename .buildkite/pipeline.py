"""Buildkite dynamic pipeline generator for qdb-api-rest.

Loads step templates from `steps/*.yml`, substitutes `{placeholder}`
vars, overlays env, the Docker plugin, and qdb-artifacts options
(variant + git_ref) per platform. Produces a 10-step graph: one lint
step in parallel with eight per-platform combined steps (build + unit
tests), then an aggregate test-report step.

The e2e harness (tests/e2e/) is deliberately NOT wired into CI
(owner decision, .buildkite/AGENTS.md); the per-platform steps do not
start qdbd or download the server/utils archives. Only the C API archive
is fetched, and the build step asserts its layout -- the artifact dance
the M1 cgo work will link against.

Usage:
    python3 pipeline.py [generate|check]
"""

from __future__ import annotations

import dataclasses
import sys
from pathlib import Path

from buildkite_sdk import CommandStep, Pipeline

# qdb-cicd-tools ships the qdb_pipeline library as a submodule.
# Insert the path before importing so the relative submodule is found
# regardless of the working directory the generator is called from.
sys.path.insert(0, str(Path(__file__).parent / "tools"))
from qdb_pipeline import (
    Platform,
    apply_docker,
    get_git_ref,
    load_template,
    merge_env,
    select_platforms,
    set_artifact_plugin_options,
    validate_pipeline,
)  # noqa: E402

STEPS_DIR = Path(__file__).parent / "steps"

# Repo-specific Platform overlays. Linux platforms run inside the rhel7
# builder container; other OSes run on bare agents (docker_image="").
# No toolchain fields are set -- Go does not need c_compiler / cxx_compiler /
# asm_compiler / ccache.
_LINUX = dict(docker_image="bureau14/builder:rhel7")
_OS_OVERLAY: dict[str, dict] = {"linux": _LINUX}

# 8-platform matrix, mirroring quasardb's own pipeline name for name
# (same matrix as qdb-nats-connector, the reference pipeline).
#
# Both amd64 baselines are built because QuasarDB publishes a core2 variant
# of every C++ artifact.  This is an ISA-compatibility requirement, not a
# performance knob: a GOAMD64=v3 binary linked against a -march=core2
# libqdb_api cannot execute on the pre-2013 hardware that variant serves.
#
# Matching quasardb's platform names also wires the C API dependency up for
# free -- the qdb-artifacts download variant in generate_pipeline() is
# Platform.slug("release"), i.e. "linux-core2-release", which is exactly the
# variant quasardb-build publishes.
PLATFORMS: list[Platform] = [
    dataclasses.replace(p, **_OS_OVERLAY.get(p.os, {}))
    for p in select_platforms(
        "linux-amd64-core2",
        "linux-amd64-haswell",
        "linux-aarch64",
        "windows-amd64-core2",
        "windows-amd64-haswell",
        "freebsd-amd64-core2",
        "freebsd-amd64-haswell",
        "macos-aarch64",
    )
]

# Environment variable layering: global -> step -> os -> os+step -> cpu.
# Empty dicts are kept on purpose so future env knobs land in the right
# slot without refactoring the merge call.
GLOBAL_ENV: dict[str, str] = {
    "AWS_DEFAULT_REGION": "eu-west-1",
}

STEP_ENV: dict[str, dict[str, str]] = {}

# Linux builds run inside bureau14/builder:rhel7, whose default /usr/bin/gcc
# is 7.3.1 -- too old to provide the aarch64 outline-atomic libgcc helpers
# (__aarch64_*_sync) that Go 1.25+ emits from its prebuilt -race (TSan)
# runtime, so linking the race-enabled test binaries fails on linux-aarch64.
# Pin CC/CXX to gcc15 (the compiler all QuasarDB artifacts are built with;
# its libgcc has the helpers) for both linux arches -- amd64 never references
# those symbols, so this is a harmless consistency win there.  The doubled $$
# escapes Buildkite's upload-time interpolation so the literal
# $QDB_CICD_AGENT_GCC15_* reaches the agent shell, which substitutes the
# concrete path (written by qdb-cloud-deployments 36-write-agent-env.sh);
# the docker plugin then propagates the resolved CC/CXX into the container.
# Same idiom as _go_env_for_agent() below.
OS_ENV: dict[str, dict[str, str]] = {
    "linux": {
        "CC": "$$QDB_CICD_AGENT_GCC15_CC",
        "CXX": "$$QDB_CICD_AGENT_GCC15_CXX",
    },
}

OS_STEP_ENV: dict[str, dict[str, str]] = {}

# Mirrors quasardb's pipeline: QDB_CPU_ARCHITECTURE_CORE2 is QuasarDB's
# canonical "legacy baseline" switch; absence means haswell.  00.common.sh's
# cicd_setup_cpu_baseline derives GOAMD64 from it (core2 -> v1, else v3), so
# a single knob drives the Go codegen level; declaring GOAMD64 here as well
# would let the two drift apart.
CPU_ENV: dict[str, dict[str, str]] = {
    "core2": {"QDB_CPU_ARCHITECTURE_CORE2": "ON"},
}

# Go version slug used to form the per-OS agent env var names.
# The slug is the major+minor with no dot: 1.27 -> "127".
# Changing this constant is the single point of control for the Go version.
GO_VERSION_SLUG = "127"  # Go 1.27


def _go_env_for_agent() -> dict[str, str]:
    """Return the GOROOT / GOPATH env vars for the current Go version.

    The Buildkite agent shell substitutes $QDB_CICD_AGENT_GO<slug>_ROOT and
    $QDB_CICD_AGENT_GO<slug>_PATH at job-start time; these vars are written
    by the per-OS packer scripts in qdb-cloud-deployments (e.g.
    agents/debian/scripts/36-write-agent-env.sh).  The doubled $$ is required
    to escape Buildkite's own variable interpolation during pipeline upload
    so that the literal string "$QDB_CICD_AGENT_GO127_ROOT" reaches the agent
    shell rather than being expanded (to an empty string) by the Buildkite
    server.  Same idiom as qdb-nats-connector and qdb-api-go.
    """
    return {
        "GOROOT": f"$$QDB_CICD_AGENT_GO{GO_VERSION_SLUG}_ROOT",
        "GOPATH": f"$$QDB_CICD_AGENT_GO{GO_VERSION_SLUG}_PATH",
    }


def _env(p: Platform, step_name: str) -> dict[str, str]:
    """Compose the full environment dict for one step.

    Layers global, per-step, per-os, per-(os, step), and per-cpu env on
    top of the platform overlay applied last by `merge_env`. This repo
    has no Release/Debug axis, so there is no `build_type` parameter.
    """
    return merge_env(
        GLOBAL_ENV,
        STEP_ENV.get(step_name, {}),
        OS_ENV.get(p.os, {}),
        OS_STEP_ENV.get(f"{p.os}/{step_name}", {}),
        CPU_ENV.get(p.cpu, {}),
        platform=p,
    )


def _lint_step() -> dict:
    """Build the standalone lint step.

    Layers the global env and injects the Go-toolchain variables from
    _go_env_for_agent() so that cicd_setup_go_toolchain in 10.lint.sh
    finds GOROOT and GOPATH at job-start time.  The step is then wrapped
    with apply_docker(bureau14/builder:rhel7) so that the propagated env
    and the qdb/ volume (populated by the qdb-artifacts plugin declared in
    _lint.yml) are both visible inside the container -- the same container
    the build steps use.  Lint runs in parallel with the per-platform
    combined steps.  Variant + git_ref for the qdb-artifacts download block
    are injected later in generate_pipeline().
    """
    step = load_template(STEPS_DIR / "_lint.yml")
    # Compose env: global baseline, then template overrides, then Go toolchain.
    # The Go env is last so the version slug in pipeline.py is authoritative.
    env = merge_env(GLOBAL_ENV, STEP_ENV.get("lint", {}))
    env.update(step.get("env") or {})
    env.update(_go_env_for_agent())
    step["env"] = env
    apply_docker(step, "bureau14/builder:rhel7")
    return step


def _per_platform_step(p: Platform) -> dict:
    """Generate the combined per-platform step (build + unit tests) for
    one platform.

    The template declares the bash invocations in its `commands:` list;
    this function handles env composition, docker overlay, and
    template-var substitution.  The Go-toolchain env (GOROOT, GOPATH) is
    injected via _go_env_for_agent() so that cicd_setup_go_toolchain in
    20.build.sh and 30.test-unit.sh can derive the correct go binary
    without relying on PATH or make.  The queue template var is
    `"{queue_os}-{arch}"` (no prefix) on macOS; the template spells
    `default-{queue}` elsewhere.  `apply_docker` is a no-op when
    `p.docker_image` is empty (non-linux platforms) so the same call works
    uniformly across all OSes.  Variant + git_ref for the qdb-artifacts
    download block are injected later in `generate_pipeline()`.
    """
    tvars = {
        "slug": p.slug(),
        "queue": (
            f"{p.queue_os}-{p.arch}"
            if p.os == "macos"
            else f"default-{p.queue_os}-{p.arch}"
        ),
    }

    step = load_template(STEPS_DIR / "_build.yml", **tvars)
    env = _env(p, "build")
    env.update(step.get("env") or {})
    # Go env last so the version slug in pipeline.py is authoritative.
    env.update(_go_env_for_agent())
    step["env"] = env
    apply_docker(step, p.docker_image, p.docker_volumes)
    return step


def generate_pipeline() -> Pipeline:
    """Assemble the full pipeline and return it.

    Resulting graph (10 steps total):
        lint (1)
        build-{slug} x8   (each running build + unit tests in sequence)
        test report (1)   (depends on every build-{slug})

    The first nine run in parallel; only the test-report step has
    depends_on.

    Each step's qdb-artifacts plugin entry receives the platform variant
    and the current git_ref for its ``download`` block; the plugin falls
    back to quasardb-build's master artifacts when this repo's branch has
    none.  There are no ``upload`` or ``promote`` blocks: this pipeline
    publishes no artifacts yet (packaging is out of the plan; see the
    brief's Non-goals).
    """
    git_ref = get_git_ref()
    pipeline = Pipeline()

    lint = _lint_step()
    # Lint is not per-platform: it needs *a* C API to typecheck against, not
    # a matching one.  Pinned to haswell (the lint agent is haswell-class).
    set_artifact_plugin_options(
        lint,
        {"download": {"variant": "linux-haswell-release", "git_ref": git_ref}},
    )
    pipeline.add_step(CommandStep.from_dict(lint))

    variants = []
    for p in PLATFORMS:
        step = _per_platform_step(p)
        variant = p.slug("release")
        variants.append(p.slug())
        set_artifact_plugin_options(
            step,
            {"download": {"variant": variant, "git_ref": git_ref}},
        )
        pipeline.add_step(CommandStep.from_dict(step))

    step = load_template(STEPS_DIR / "_test_report.yml")
    step["depends_on"] = [f"build-{variant}" for variant in variants]
    pipeline.add_step(CommandStep.from_dict(step))

    return pipeline


def main() -> None:
    """Entry point.

    Commands:
        generate  -- emit the pipeline YAML to stdout (default).
        check     -- validate pipeline structure; print errors or [OK] summary.
    """
    command = sys.argv[1] if len(sys.argv) > 1 else "generate"

    try:
        pipeline = generate_pipeline()
    except Exception as e:
        print(f"[FAIL] Pipeline generation failed: {e}", file=sys.stderr)
        sys.exit(1)

    if command == "generate":
        print(pipeline.to_yaml())
    elif command == "check":
        errors = validate_pipeline(pipeline)
        if errors:
            for e in errors:
                print(f"[FAIL] {e}", file=sys.stderr)
            sys.exit(1)
        print(f"[OK] Pipeline valid: {len(pipeline.steps)} steps")
    else:
        print(f"Unknown command: {command}", file=sys.stderr)
        print("Usage: pipeline.py [generate|check]", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
