"""Check image-pull references using Helm's render and schema validation."""

import copy
import json
import subprocess
import unittest
from pathlib import Path

import yaml

CHART = Path(__file__).resolve().parents[2] / "deploy/kubernetes/chart"
FIXTURE = CHART / "ci/values.yaml"


def template(references=None, *, use_fixture=True):
    command = ["helm", "template", "kallisti", str(CHART), "-n", "kallisti-test"]
    if use_fixture:
        command.extend(["-f", str(FIXTURE)])
    if references is not None:
        command.extend(["--set-json", "imagePullSecrets=" + json.dumps(references)])
    return subprocess.run(command, text=True, capture_output=True, check=False)


class ImagePullSecretsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        baseline = template()
        if baseline.returncode != 0:
            raise RuntimeError(baseline.stderr)
        cls.baseline = list(yaml.safe_load_all(baseline.stdout))
        cls.deployment = next(
            item for item in cls.baseline if item["kind"] == "Deployment"
        )

    def check_references(self, references):
        rendered = template(references)
        self.assertEqual(rendered.returncode, 0, rendered.stderr)
        objects = list(yaml.safe_load_all(rendered.stdout))
        self.assertEqual(
            [item["kind"] for item in objects], [item["kind"] for item in self.baseline]
        )
        self.assertFalse(
            any(item["kind"] in ("Secret", "ServiceAccount") for item in objects)
        )
        deployment = next(item for item in objects if item["kind"] == "Deployment")
        pod = deployment["spec"]["template"]["spec"]
        self.assertNotIn("imagePullSecrets", deployment["spec"])
        self.assertTrue(
            all("imagePullSecrets" not in container for container in pod["containers"])
        )
        self.assertEqual(
            [container["name"] for container in pod["containers"]],
            ["kallisti", "caddy", "cert-sync"],
        )
        self.assertFalse(pod["automountServiceAccountToken"])
        if references:
            self.assertEqual(pod["imagePullSecrets"], references)
        else:
            self.assertNotIn("imagePullSecrets", pod)
        unchanged = copy.deepcopy(deployment)
        unchanged["spec"]["template"]["spec"].pop("imagePullSecrets", None)
        self.assertEqual(unchanged, self.deployment)
        for original, current in zip(self.baseline, objects):
            if original["kind"] != "Deployment":
                self.assertEqual(current, original)

    def test_empty_and_multiple_references(self):
        self.assertNotIn(
            "imagePullSecrets", self.deployment["spec"]["template"]["spec"]
        )
        self.check_references([])
        self.check_references([{"name": "regcred"}])
        self.check_references([{"name": "regcred"}, {"name": "other-registry"}])

    def test_invalid_references(self):
        for references in (
            "regcred",
            ["regcred"],
            [{}],
            [{"name": ""}],
            [{"name": "   "}],
            [{"name": "Invalid_Name"}],
            [{"name": "bad..name"}],
            [{"name": "regcred", "password": "no"}],
        ):
            with self.subTest(references=references):
                rendered = template(references)
                self.assertNotEqual(rendered.returncode, 0)
                self.assertIn("imagePullSecrets", rendered.stderr)

    def test_default_values_still_fail(self):
        rendered = template(use_fixture=False)
        self.assertNotEqual(rendered.returncode, 0)
        self.assertIn("Set host", rendered.stderr)


class PrivateRoomMediaTest(unittest.TestCase):
    def test_opt_in_only(self):
        for value, enabled in (("false", False), ("true", True)):
            with self.subTest(value=value):
                rendered = subprocess.run(
                    ["helm", "template", "kallisti", str(CHART), "-f", str(FIXTURE),
                     "--set", "experimentalPrivateRoomMedia=" + value],
                    text=True, capture_output=True, check=False,
                )
                self.assertEqual(rendered.returncode, 0, rendered.stderr)
                deployment = next(item for item in yaml.safe_load_all(rendered.stdout)
                                  if item["kind"] == "Deployment")
                containers = deployment["spec"]["template"]["spec"]["containers"]
                for container in containers:
                    flags = [entry for entry in container.get("env", [])
                             if entry["name"] == "EXPERIMENTAL_PRIVATE_ROOM_MEDIA"]
                    expected = ([{"name": "EXPERIMENTAL_PRIVATE_ROOM_MEDIA", "value": "1"}]
                                if enabled and container["name"] == "kallisti" else [])
                    self.assertEqual(flags, expected)

    def test_rejects_string_boolean(self):
        rendered = subprocess.run(
            ["helm", "template", "kallisti", str(CHART), "-f", str(FIXTURE),
             "--set-string", "experimentalPrivateRoomMedia=false"],
            text=True, capture_output=True, check=False,
        )
        self.assertNotEqual(rendered.returncode, 0)
        self.assertIn("experimentalPrivateRoomMedia", rendered.stderr)


if __name__ == "__main__":
    unittest.main()
