"""Exercise deploy.sh with fake Docker/curl; never contacts Docker or Telegram."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
BASH = os.environ.get("DEPLOY_TEST_BASH") or shutil.which("bash")
if not BASH:
    BASH = next((p for p in ("C:/Program Files/Git/bin/bash.exe", "C:/msys64/usr/bin/bash.exe") if Path(p).is_file()), None)

DOCKER = r'''#!/usr/bin/env bash
set -eu
case "$1" in
  info) [[ "${MOCK_DOCKER_DOWN:-0}" == 0 ]]; exit ;;
  ps)
    if [[ "${MOCK_INSTALLED:-0}" == 1 && "$*" != *"other-directory"* ]]; then printf 'bot-id\npg-id\n'; fi
    exit ;;
  volume) [[ "${MOCK_VOLUME:-0}" != 1 ]] || echo database-volume; exit 0 ;;
  inspect)
    case "$3" in
      *compose.service*) if [[ "$4" == bot-id ]]; then echo bot; else echo postgres; fi ;;
      *compose.project*) echo "${MOCK_PROJECT:-deployment}" ;;
      *Health.Status*) echo healthy ;;
      *State.Status*) echo running ;;
      *Config.Image*) cat "$MOCK_DIR/image" ;;
      *) exit 1 ;;
    esac
    exit ;;
  compose) shift ;;
  *) echo "unsupported docker command" >&2; exit 9 ;;
esac
if [[ "$1" == version ]]; then
  [[ "${MOCK_COMPOSE_V1:-0}" == 0 || "${MOCK_V1_INVOKED:-0}" == 1 ]]
  exit
fi
printf '%s\n' "$*" >> "$MOCK_DIR/docker.log"
config_file= env_file= project=
while [[ "$1" == -* ]]; do
  case "$1" in
    -f) config_file="$2"; shift 2 ;;
    --env-file) env_file="$2"; shift 2 ;;
    -p) project="$2"; shift 2 ;;
    --project-directory) shift 2 ;;
    *) exit 8 ;;
  esac
done
case "$1" in
  config) [[ "${MOCK_CONFIG_FAIL:-0}" == 0 ]]; exit ;;
  pull) [[ "${MOCK_PULL_FAIL:-0}" == 0 ]]; exit ;;
  up)
    [[ -z "${POSTGRES_PASSWORD+x}" && -z "${BOT_TOKEN+x}" ]] || { echo 'ambient credentials leaked into Compose' >&2; exit 7; }
    sed -n 's/^BOT_IMAGE=//p' "$env_file" | tail -n 1 > "$MOCK_DIR/image"
    printf '%s' "$project" > "$MOCK_DIR/project"
    exit ;;
  ps) if [[ "$*" == *postgres* ]]; then echo pg-id; else echo bot-id; fi ;;
  *) exit 8 ;;
esac
'''

CURL = r'''#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$MOCK_DIR/curl.log"
out= url=
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w|--retry|--connect-timeout|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  */releases/latest)
    [[ "${MOCK_RELEASE_FAIL:-0}" == 0 ]] || exit 22
    printf 'https://github.com/kexue-aihao/Telegram_Adblock_transmit/releases/tag/%s' "${MOCK_RELEASE:-v1.9.1}"
    ;;
  */docker-compose.pull.yml)
    [[ "${MOCK_DOWNLOAD_FAIL:-0}" == 0 ]] || { echo partial > "$out"; exit 22; }
    cp "$MOCK_SOURCE/docker-compose.pull.yml" "$out" ;;
  */.env.example) cp "$MOCK_SOURCE/.env.example" "$out" ;;
  *) exit 22 ;;
esac
'''


@unittest.skipUnless(BASH, "Bash is required")
class DeployTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="telegram-deploy-test-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.deploy = self.base / "deployment"
        self.deploy.mkdir()
        self.bin = self.base / "bin"
        self.bin.mkdir()
        for name, content in {
            "docker": DOCKER,
            "curl": CURL,
            "sleep": "#!/usr/bin/env bash\nexit 0\n",
            "docker-compose": '#!/usr/bin/env bash\nMOCK_V1_INVOKED=1 exec docker compose "$@"\n',
        }.items():
            target = self.bin / name
            target.write_text(content, encoding="utf-8", newline="\n")
            target.chmod(0o700)
        self.env = os.environ.copy()
        for key in ("BOT_TOKEN", "POSTGRES_PASSWORD", "WEBUI_ENABLE", "WEBUI_ADDR", "WEBUI_USERNAME", "WEBUI_PASSWORD", "WEBUI_SESSION_SECRET", "BIO_CHECK_ENABLED", "BOT_OWNER_IDS", "BOT_IMAGE", "RAW_BASE", "RELEASE_VERSION", "COMPOSE_PROJECT_NAME", "BASH_ENV"):
            self.env.pop(key, None)
        self.env.update(DEPLOY_DIR=self.deploy.as_posix(), MOCK_DIR=self.base.as_posix(), MOCK_SOURCE=ROOT.as_posix())
        # Bash receives PATH after converting its own Windows environment.
        self.command = [BASH, "-c", 'mock_bin="$MOCK_DIR/bin"; if command -v cygpath >/dev/null; then mock_bin="$(cygpath -u "$mock_bin")"; fi; export PATH="$mock_bin:$PATH"; bash "$MOCK_SOURCE/scripts/deploy.sh"']

    def existing(self, *, image="ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.8.0", panel=True):
        self.env["MOCK_INSTALLED"] = "1"
        self.original = (
            f"BOT_IMAGE={image}\nBOT_TOKEN=old-token\nPOSTGRES_PASSWORD=old-db-password\n"
            "TELEGRAM_API_ENDPOINT=https://example.org/bot%s/%s\nBIO_CHECK_ENABLED=true\n"
            "ADFILTER_ENABLED=false\nWEBUI_USERNAME=old-admin\nWEBUI_PASSWORD=old-panel-password\n"
            "WEBUI_SESSION_SECRET=old-secret\n"
            f"WEBUI_ADDR={'0.0.0.0:9090' if panel else ''}\n"
        )
        (self.deploy / ".env").write_text(self.original, encoding="utf-8")
        (self.deploy / "docker-compose.pull.yml").write_text("original compose\n", encoding="utf-8")

    def run_deploy(self, ok=True, stdin=""):
        result = subprocess.run(self.command, env=self.env, input=stdin, capture_output=True, text=True, encoding="utf-8", timeout=30)
        if ok:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        for secret in ("old-token", "old-db-password", "old-panel-password", "old-secret"):
            self.assertNotIn(secret, result.stdout + result.stderr)
        return result

    def values(self):
        return dict(line.split("=", 1) for line in (self.deploy / ".env").read_text(encoding="utf-8").splitlines() if line and not line.startswith("#") and "=" in line)

    def log(self, name):
        path = self.base / name
        return path.read_text(encoding="utf-8") if path.exists() else ""

    def test_fresh_install_pins_latest_release(self):
        self.env.update(BOT_TOKEN="new-token", POSTGRES_PASSWORD="new-password", WEBUI_ENABLE="0")
        result = self.run_deploy()
        self.assertIn("初始安装完成", result.stdout)
        self.assertTrue(self.values()["BOT_IMAGE"].endswith(":v1.9.1"))
        self.assertIn("/v1.9.1/docker-compose.pull.yml", self.log("curl.log"))
        self.assertFalse((self.deploy / ".backups").exists())

    def test_upgrade_preserves_credentials_panel_and_project(self):
        self.existing()
        self.env.update(MOCK_PROJECT="my-panel-project", BOT_TOKEN="ambient-token", POSTGRES_PASSWORD="ambient-password")
        result = self.run_deploy()
        self.assertIn("升级完成", result.stdout)
        values = self.values()
        for key, value in dict(line.split("=", 1) for line in self.original.splitlines()).items():
            if key != "BOT_IMAGE": self.assertEqual(values[key], value)
        self.assertEqual(values["COMPOSE_PROJECT_NAME"], "my-panel-project")
        self.assertTrue(values["BOT_IMAGE"].endswith(":v1.9.1"))
        backups = list((self.deploy / ".backups").iterdir())
        self.assertEqual(len(backups), 1)
        self.assertEqual((backups[0] / ".env").read_text(encoding="utf-8"), self.original)
        self.assertEqual((backups[0] / "docker-compose.pull.yml").read_text(), "original compose\n")
        log = self.log("docker.log")
        self.assertNotIn("--force-recreate", log)
        self.assertNotIn("down", log)
        self.assertIn("-p my-panel-project", log)
        self.assertEqual(self.log("project"), "my-panel-project")
        self.run_deploy()  # Repeated invocation stays noninteractive.

    def test_disabled_panel_stays_disabled(self):
        self.existing(panel=False)
        self.run_deploy()
        self.assertEqual(self.values()["WEBUI_ADDR"], "")

    def test_explicit_image_overrides_saved_image(self):
        self.existing()
        self.env.update(BOT_IMAGE="ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.9.0", WEBUI_ENABLE="0", BIO_CHECK_ENABLED="false")
        self.run_deploy()
        self.assertTrue(self.values()["BOT_IMAGE"].endswith(":v1.9.0"))
        self.assertEqual(self.values()["BIO_CHECK_ENABLED"], "false")
        self.assertEqual(self.values()["WEBUI_ADDR"], "")
        self.assertNotIn("releases/latest", self.log("curl.log"))

    def test_owner_ids_are_validated_and_saved(self):
        self.existing()
        self.env.update(BOT_OWNER_IDS="123456789, 987654321")
        self.run_deploy()
        self.assertEqual(self.values()["BOT_OWNER_IDS"], "123456789, 987654321")

        self.env.update(BOT_OWNER_IDS="not-a-number")
        result = self.run_deploy(ok=False)
        self.assertIn("BOT_OWNER_IDS", result.stdout + result.stderr)
        self.assertNotIn("not-a-number", (self.deploy / ".env").read_text(encoding="utf-8"))

    def test_explicit_release_and_legacy_compose(self):
        self.existing()
        self.env.update(RELEASE_VERSION="v1.9.0", MOCK_COMPOSE_V1="1")
        self.run_deploy()
        self.assertTrue(self.values()["BOT_IMAGE"].endswith(":v1.9.0"))
        self.assertNotIn("releases/latest", self.log("curl.log"))

    def test_custom_image_is_retained(self):
        self.existing(image="registry.example/custom-bot:stable")
        self.run_deploy()
        self.assertEqual(self.values()["BOT_IMAGE"], "registry.example/custom-bot:stable")
        self.assertNotIn("releases/latest", self.log("curl.log"))

    def test_retained_volume_without_env_stops(self):
        self.env.update(MOCK_VOLUME="1")
        self.run_deploy(ok=False)
        self.assertFalse((self.deploy / ".env").exists())
        self.assertEqual(self.log("docker.log"), "")

    def test_env_without_containers_restores_deployment(self):
        self.existing()
        self.env["MOCK_INSTALLED"] = "0"
        self.assertIn("升级完成", self.run_deploy().stdout)

    def test_failures_leave_existing_files_and_services_untouched(self):
        self.existing()
        for failure in ("MOCK_RELEASE_FAIL", "MOCK_DOWNLOAD_FAIL", "MOCK_CONFIG_FAIL", "MOCK_PULL_FAIL", "MOCK_DOCKER_DOWN"):
            with self.subTest(failure=failure):
                self.env[failure] = "1"
                self.run_deploy(ok=False)
                self.env.pop(failure)
                self.assertEqual((self.deploy / ".env").read_text(encoding="utf-8"), self.original)
                self.assertEqual((self.deploy / "docker-compose.pull.yml").read_text(), "original compose\n")
                self.assertNotIn(" up ", self.log("docker.log"))
                self.assertFalse(list(self.deploy.glob(".deploy.*")))

    def test_conflicting_project_stops_before_download(self):
        self.existing()
        self.env["COMPOSE_PROJECT_NAME"] = "different-project"
        self.run_deploy(ok=False)
        self.assertEqual(self.log("curl.log"), "")

    def test_invalid_target_stops_before_mutation(self):
        self.existing()
        for key, value in (("RELEASE_VERSION", "bad/tag"), ("BOT_IMAGE", "")):
            self.env[key] = value
            self.run_deploy(ok=False)
            self.env.pop(key)
            self.assertEqual((self.deploy / ".env").read_text(encoding="utf-8"), self.original)


if __name__ == "__main__":
    unittest.main()
