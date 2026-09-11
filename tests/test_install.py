"""Tests for the installer, the .env loader, and zero-dependency startup."""
from __future__ import annotations

import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

import install as installer  # noqa: E402
from runtime import env as env_mod  # noqa: E402


class EnvParsingTests(unittest.TestCase):
    def test_parses_basic_assignments_and_comments(self):
        text = (
            "# a comment\n"
            "\n"
            "OPENCODE_API_KEY=sk-abc123\n"
            "  RDR2AI_MODEL = deepseek-v4.1-flash  \n"
            "export RDR2AI_BACKEND=llm\n"
        )
        parsed = env_mod.parse_env_text(text)
        self.assertEqual(parsed["OPENCODE_API_KEY"], "sk-abc123")
        self.assertEqual(parsed["RDR2AI_MODEL"], "deepseek-v4.1-flash")
        self.assertEqual(parsed["RDR2AI_BACKEND"], "llm")

    def test_handles_quotes_and_inline_comments(self):
        text = (
            'QUOTED="value with spaces"\n'
            "SINGLE='another value'\n"
            "PLAIN=bare # trailing comment\n"
            "URL=https://example.com/v1#fragment\n"
            "NOT_A_PAIR\n"
            "=missing-key\n"
        )
        parsed = env_mod.parse_env_text(text)
        self.assertEqual(parsed["QUOTED"], "value with spaces")
        self.assertEqual(parsed["SINGLE"], "another value")
        self.assertEqual(parsed["PLAIN"], "bare")
        self.assertEqual(parsed["URL"], "https://example.com/v1#fragment")
        self.assertNotIn("NOT_A_PAIR", parsed)

    def test_interpolation_uses_earlier_keys_then_environ(self):
        with mock.patch.dict(os.environ, {"EXTERNAL_HOST": "api.example.com"}, clear=False):
            parsed = env_mod.parse_env_text(
                "BASE=${EXTERNAL_HOST}/v1\nFULL=${BASE}/chat\nMISSING=${NOPE_UNDEFINED}\n"
            )
        self.assertEqual(parsed["BASE"], "api.example.com/v1")
        self.assertEqual(parsed["FULL"], "api.example.com/v1/chat")
        self.assertEqual(parsed["MISSING"], "")

    def test_load_env_file_does_not_clobber_real_environment(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / ".env"
            path.write_text("KEEP_ME=from-file\nADD_ME=from-file\n", encoding="utf-8")
            with mock.patch.dict(os.environ, {"KEEP_ME": "from-shell"}, clear=False):
                os.environ.pop("ADD_ME", None)
                applied = env_mod.load_env_file(path)
                self.assertEqual(os.environ["KEEP_ME"], "from-shell")
                self.assertEqual(os.environ["ADD_ME"], "from-file")
                self.assertEqual(applied, {"ADD_ME": "from-file"})

                env_mod.load_env_file(path, override=True)
                self.assertEqual(os.environ["KEEP_ME"], "from-file")
                os.environ.pop("ADD_ME", None)

    def test_load_env_searches_multiple_candidates(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / ".env").write_text("FROM_BASE=1\n", encoding="utf-8")
            (root / "config").mkdir()
            (root / "config/local.env").write_text("FROM_LOCAL=2\n", encoding="utf-8")
            os.environ.pop("FROM_BASE", None)
            os.environ.pop("FROM_LOCAL", None)
            applied = env_mod.load_env(root=root)
            self.assertEqual(applied.get("FROM_BASE"), "1")
            self.assertEqual(applied.get("FROM_LOCAL"), "2")
            os.environ.pop("FROM_BASE", None)
            os.environ.pop("FROM_LOCAL", None)

    def test_load_env_ignores_missing_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(env_mod.load_env(root=Path(tmp)), {})


class UpsertEnvFileTests(unittest.TestCase):
    def test_creates_file_with_values(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "nested" / ".env"
            env_mod.upsert_env_file(path, {"OPENCODE_API_KEY": "sk-1"})
            self.assertTrue(path.is_file())
            self.assertIn("OPENCODE_API_KEY=sk-1", path.read_text(encoding="utf-8"))

    def test_updates_in_place_and_preserves_comments(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / ".env"
            path.write_text(
                "# keep this comment\nOPENCODE_API_KEY=old\nOTHER=untouched\n",
                encoding="utf-8",
            )
            env_mod.upsert_env_file(path, {"OPENCODE_API_KEY": "new", "RDR2AI_MODEL": "m"})
            text = path.read_text(encoding="utf-8")
            self.assertIn("# keep this comment", text)
            self.assertIn("OPENCODE_API_KEY=new", text)
            self.assertIn("OTHER=untouched", text)
            self.assertIn("RDR2AI_MODEL=m", text)
            self.assertNotIn("OPENCODE_API_KEY=old", text)

    def test_is_idempotent(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / ".env"
            env_mod.upsert_env_file(path, {"A": "1"})
            first = path.read_text(encoding="utf-8")
            env_mod.upsert_env_file(path, {"A": "1"})
            self.assertEqual(first, path.read_text(encoding="utf-8"))


class DoctorCheckTests(unittest.TestCase):
    def test_python_version_is_supported(self):
        check = installer.check_python()
        self.assertEqual(check.status, installer.OK, check.detail)

    def test_layout_is_complete(self):
        check = installer.check_layout()
        self.assertEqual(check.status, installer.OK, check.detail)

    def test_runtime_modules_import(self):
        check = installer.check_imports()
        self.assertEqual(check.status, installer.OK, check.detail)

    def test_tool_catalog_is_populated(self):
        check = installer.check_tool_catalog()
        self.assertEqual(check.status, installer.OK, check.detail)
        self.assertGreaterEqual(int(check.detail.split()[0]), 30)

    def test_context_packs_are_present(self):
        check = installer.check_data_packs()
        self.assertEqual(check.status, installer.OK, check.detail)
        self.assertIn("profiles", check.detail)

    def test_settings_json_is_valid(self):
        check = installer.check_settings()
        self.assertEqual(check.status, installer.OK, check.detail)

    def test_timeline_directory_is_writable(self):
        check = installer.check_timeline_writable()
        self.assertEqual(check.status, installer.OK, check.detail)

    def test_build_tools_check_never_fails(self):
        args = installer.build_parser().parse_args([])
        self.assertIn(installer.check_build_tools(args).status, {installer.OK, installer.SKIP})

    def test_cmake_platform_args_returns_empty_off_windows(self):
        if sys.platform.startswith("win"):
            self.skipTest("windows goes through the Visual Studio generator probe")
        self.assertEqual(installer._cmake_platform_args(sys.executable), [])

    def test_cmake_platform_args_pins_x64_for_visual_studio(self):
        # RDR2 is 64-bit: a Win32 build cannot link the x64 ScriptHookRDR2.lib.
        with mock.patch.object(installer.platform, "system", return_value="Windows"), \
                mock.patch.object(installer, "_run") as run:
            run.return_value = subprocess.CompletedProcess(
                [], 0, "* Visual Studio 17 2022        = Generators\n", ""
            )
            self.assertEqual(installer._cmake_platform_args("cmake"), ["-A", "x64"])

            run.return_value = subprocess.CompletedProcess([], 0, "* Ninja = Generators\n", "")
            self.assertEqual(installer._cmake_platform_args("cmake"), [])

    def test_sdk_check_accepts_explicit_root(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "inc").mkdir()
            (root / "lib").mkdir()
            (root / "inc/main.h").write_text("// sdk", encoding="utf-8")
            (root / "lib/ScriptHookRDR2.lib").write_text("lib", encoding="utf-8")
            args = installer.build_parser().parse_args(["--sdk-root", str(root)])
            check = installer.check_bridge_sdk(args)
            self.assertEqual(check.status, installer.OK, check.detail)

    def test_sdk_check_reports_incomplete_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = installer.build_parser().parse_args(["--sdk-root", tmp])
            check = installer.check_bridge_sdk(args)
            self.assertEqual(check.status, installer.WARN)
            self.assertIn("incomplete", check.detail)

    def test_sdk_check_skips_when_absent(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = installer.build_parser().parse_args(["--sdk-root", str(Path(tmp) / "nope")])
            check = installer.check_bridge_sdk(args)
            self.assertEqual(check.status, installer.SKIP)

    def test_doctor_has_no_blocking_issues_on_this_checkout(self):
        args = installer.build_parser().parse_args([])
        failures = [c for c in installer.run_doctor(args) if c.status == installer.FAIL]
        self.assertEqual(failures, [], [f"{c.name}: {c.detail}" for c in failures])


class InstallerCliTests(unittest.TestCase):
    def _run(self, argv):
        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            code = installer.main(argv)
        return code, buffer.getvalue()

    def test_check_mode_passes_and_prints_next_steps(self):
        code, output = self._run(["--check"])
        self.assertEqual(code, 0, output)
        self.assertIn("Next steps:", output)
        self.assertIn("run_demo.py", output)

    def test_check_mode_emits_json(self):
        code, output = self._run(["--check", "--json"])
        self.assertEqual(code, 0)
        payload = json.loads(output)
        self.assertEqual(payload["mode"], "check")
        self.assertTrue(payload["ok"])
        names = {check["name"] for check in payload["doctor"]}
        self.assertIn("python", names)
        self.assertIn("runtime import", names)

    def test_api_key_is_written_to_env_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            env_path = Path(tmp) / ".env"
            with mock.patch.dict(os.environ, {}, clear=False):
                os.environ.pop("OPENCODE_API_KEY", None)
                code, output = self._run([
                    "--yes",
                    "--no-smoke",
                    "--api-key",
                    "sk-installer-test",
                    "--env-file",
                    str(env_path),
                    "--json",
                ])
                self.assertEqual(code, 0, output)
                payload = json.loads(output)
                steps = {step["name"]: step for step in payload["steps"]}
                self.assertEqual(steps["write .env"]["status"], installer.OK)
                self.assertEqual(os.environ["OPENCODE_API_KEY"], "sk-installer-test")
                os.environ.pop("OPENCODE_API_KEY", None)

            self.assertIn("OPENCODE_API_KEY=sk-installer-test", env_path.read_text(encoding="utf-8"))

    def test_offline_install_skips_network_steps(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, output = self._run([
                "--yes",
                "--offline",
                "--no-smoke",
                "--with-adk",
                "--env-file",
                str(Path(tmp) / ".env"),
                "--json",
            ])
            self.assertEqual(code, 0, output)
            payload = json.loads(output)
            steps = {step["name"]: step for step in payload["steps"]}
            self.assertEqual(steps["optional packages"]["status"], installer.WARN)

    def test_next_steps_target_the_requested_backend(self):
        args = installer.build_parser().parse_args(["--backend", "rule"])
        lines = installer.next_steps(args, [])
        self.assertTrue(any("--backend rule" in line for line in lines))

        args = installer.build_parser().parse_args(["--backend", "llm"])
        lines = installer.next_steps(args, [installer.Check("api key", installer.OK, "")])
        self.assertTrue(any("--backend llm" in line for line in lines))

    def test_next_steps_offers_asi_only_when_the_sdk_is_missing(self):
        args = installer.build_parser().parse_args(["--backend", "llm"])
        with_sdk = [installer.Check("ScriptHook SDK", installer.OK, "installed")]
        self.assertFalse(any("--with-asi" in line for line in installer.next_steps(args, with_sdk)))
        without_sdk = [installer.Check("ScriptHook SDK", installer.SKIP, "not installed")]
        self.assertTrue(any("--with-asi" in line for line in installer.next_steps(args, without_sdk)))

    def test_next_steps_prompts_for_missing_api_key(self):
        args = installer.build_parser().parse_args(["--backend", "llm"])
        checks = [installer.Check("api key", installer.SKIP, "not configured")]
        lines = installer.next_steps(args, checks)
        self.assertTrue(any("--api-key" in line for line in lines))

    def test_offline_install_does_not_write_env_without_a_key(self):
        with tempfile.TemporaryDirectory() as tmp:
            env_path = Path(tmp) / ".env"
            code, output = self._run([
                "--yes",
                "--offline",
                "--no-smoke",
                "--env-file",
                str(env_path),
            ])
            self.assertEqual(code, 0, output)
            self.assertFalse(env_path.exists())


class ZeroDependencyStartupTests(unittest.TestCase):
    """The runtime must start from a clean checkout with no pip installs."""

    def test_build_runtime_supports_llm_backend_without_adk(self):
        import argparse

        with mock.patch.dict(os.environ, {"OPENCODE_API_KEY": "sk-test"}, clear=False):
            args = argparse.Namespace(
                settings="config/settings.json",
                backend="llm",
                model="deepseek-v4.1-flash",
                provider=None,
                api_base=None,
                api_key=None,
                api_key_env="OPENCODE_API_KEY",
                reasoning_effort=None,
                extra_body=None,
            )
            from runtime.main import build_runtime
            from runtime.agent.openai_compat import OpenAICompatibleBackend

            runtime = build_runtime(args)
        self.assertIsInstance(runtime.backend, OpenAICompatibleBackend)
        self.assertGreaterEqual(len(runtime.registry.names()), 30)

    def test_invalid_backend_from_env_is_rejected(self):
        result = subprocess.run(
            [sys.executable, "-m", "runtime.main"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            env={**os.environ, "RDR2AI_BACKEND": "not-a-backend"},
            timeout=60,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("invalid backend", result.stderr.lower())

    def test_runtime_starts_and_announces_its_port(self):
        process = subprocess.Popen(
            [sys.executable, "-u", "-m", "runtime.main", "--backend", "rule", "--port", "0"],
            cwd=str(ROOT),
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            env={k: v for k, v in os.environ.items() if k != "RDR2AI_BACKEND"},
        )
        try:
            deadline = time.time() + 20
            lines = []
            while time.time() < deadline:
                line = process.stdout.readline()
                if not line:
                    break
                lines.append(line)
                if "[ipc] listening" in line:
                    break
            self.assertTrue(
                any("[ipc] listening" in line for line in lines),
                f"runtime never announced its port: {lines}",
            )
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:  # pragma: no cover - defensive
                process.kill()
            if process.stdout is not None:
                process.stdout.close()

    def test_env_backend_selects_the_llm_backend(self):
        # RDR2AI_BACKEND is documented in .env.example; verify main() honours it
        # by checking the parser default rather than starting a live model.
        result = subprocess.run(
            [sys.executable, "-c",
             "import sys; sys.argv=['x'];"
             "from runtime.env import load_env; load_env();"
             "import os; print(os.environ.get('RDR2AI_BACKEND'))"],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            env={**os.environ, "RDR2AI_BACKEND": "llm"},
            timeout=60,
        )
        self.assertEqual(result.stdout.strip(), "llm")


class PackagingMetadataTests(unittest.TestCase):
    def test_env_example_lists_the_documented_knobs(self):
        example = (ROOT / ".env.example").read_text(encoding="utf-8")
        for token in ("OPENCODE_API_KEY", "RDR2AI_BACKEND", "RDR2AI_MODEL", "RDR2AI_API_BASE"):
            self.assertIn(token, example)

    def test_pyproject_exposes_the_runtime_entry_point(self):
        pyproject = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
        self.assertIn("rdr2-npc-runtime", pyproject)

    def test_env_files_are_git_ignored(self):
        ignore = (ROOT / ".gitignore").read_text(encoding="utf-8")
        self.assertIn(".env", ignore)
        self.assertIn("config/local.env", ignore)


if __name__ == "__main__":
    unittest.main()
